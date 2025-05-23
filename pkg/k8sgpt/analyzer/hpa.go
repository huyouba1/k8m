/*
Copyright 2023 The K8sGPT Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package analyzer

import (
	"fmt"

	"github.com/huyouba1/k8m/pkg/k8sgpt/common"
	"github.com/huyouba1/k8m/pkg/k8sgpt/kubernetes"
	"github.com/huyouba1/k8m/pkg/k8sgpt/util"
	"github.com/weibaohui/kom/kom"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type HpaAnalyzer struct{}

func (HpaAnalyzer) Analyze(a common.Analyzer) ([]common.Result, error) {

	kind := "HorizontalPodAutoscaler"
	apiDoc := kubernetes.K8sApiReference{
		Kind: kind,
		ApiVersion: schema.GroupVersion{
			Group:   "autoscaling",
			Version: "v1",
		},
		OpenapiSchema: a.OpenapiSchema,
	}

	AnalyzerErrorsMetric.DeletePartialMatch(map[string]string{
		"analyzer_name": kind,
	})

	var list []*autoscalingv2.HorizontalPodAutoscaler
	err := kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&autoscalingv2.HorizontalPodAutoscaler{}).Namespace(a.Namespace).WithLabelSelector(a.LabelSelector).List(&list).Error

	if err != nil {
		return nil, err
	}

	var preAnalysis = map[string]common.PreAnalysis{}

	for _, hpa := range list {
		var failures []common.Failure

		// check the error from status field
		conditions := hpa.Status.Conditions
		for _, condition := range conditions {
			if condition.Status != "True" {
				failures = append(failures, common.Failure{
					Text:      condition.Message,
					Sensitive: []common.Sensitive{},
				})
			}
		}

		// check ScaleTargetRef exist
		scaleTargetRef := hpa.Spec.ScaleTargetRef
		var podInfo PodInfo

		switch scaleTargetRef.Kind {
		case "Deployment":
			var deployment *appsv1.Deployment
			err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&appsv1.Deployment{}).Namespace(a.Namespace).Name(scaleTargetRef.Name).Get(&deployment).Error
			if err == nil {
				podInfo = DeploymentInfo{deployment}
			}
		case "ReplicationController":
			var rc *corev1.ReplicationController
			err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&corev1.ReplicationController{}).Namespace(a.Namespace).Name(scaleTargetRef.Name).Get(&rc).Error
			if err == nil {
				podInfo = ReplicationControllerInfo{rc}
			}
		case "ReplicaSet":
			var rs *appsv1.ReplicaSet
			err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&appsv1.ReplicaSet{}).Namespace(a.Namespace).Name(scaleTargetRef.Name).Get(&rs).Error
			if err == nil {
				podInfo = ReplicaSetInfo{rs}
			}
		case "StatefulSet":
			var ss *appsv1.StatefulSet
			err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&appsv1.StatefulSet{}).Namespace(a.Namespace).Name(scaleTargetRef.Name).Get(&ss).Error
			if err == nil {
				podInfo = StatefulSetInfo{ss}
			}
		default:
			failures = append(failures, common.Failure{
				Text:      fmt.Sprintf("HorizontalPodAutoscaler uses %s as ScaleTargetRef which is not an option.", scaleTargetRef.Kind),
				Sensitive: []common.Sensitive{},
			})
		}

		if podInfo == nil {
			doc := apiDoc.GetApiDocV2("spec.scaleTargetRef")

			failures = append(failures, common.Failure{
				Text:          fmt.Sprintf("HorizontalPodAutoscaler uses %s/%s as ScaleTargetRef which does not exist.", scaleTargetRef.Kind, scaleTargetRef.Name),
				KubernetesDoc: doc,
				Sensitive: []common.Sensitive{
					{
						Unmasked: scaleTargetRef.Name,
						Masked:   util.MaskString(scaleTargetRef.Name),
					},
				},
			})
		} else {
			containers := len(podInfo.GetPodSpec().Containers)
			for _, container := range podInfo.GetPodSpec().Containers {
				if container.Resources.Requests == nil || container.Resources.Limits == nil {
					containers--
				}
			}

			if containers <= 0 {
				doc := apiDoc.GetApiDocV2("spec.scaleTargetRef.kind")

				failures = append(failures, common.Failure{
					Text:          fmt.Sprintf("%s %s/%s does not have resource configured.", scaleTargetRef.Kind, a.Namespace, scaleTargetRef.Name),
					KubernetesDoc: doc,
					Sensitive: []common.Sensitive{
						{
							Unmasked: scaleTargetRef.Name,
							Masked:   util.MaskString(scaleTargetRef.Name),
						},
					},
				})
			}

		}

		if len(failures) > 0 {
			preAnalysis[fmt.Sprintf("%s/%s", hpa.Namespace, hpa.Name)] = common.PreAnalysis{
				HorizontalPodAutoscalers: *hpa,
				FailureDetails:           failures,
			}
			AnalyzerErrorsMetric.WithLabelValues(kind, hpa.Name, hpa.Namespace).Set(float64(len(failures)))
		}

	}

	for key, value := range preAnalysis {
		var currentAnalysis = common.Result{
			Kind:  kind,
			Name:  key,
			Error: value.FailureDetails,
		}

		parent, found := util.GetParent(a.Context, a.ClusterID, value.HorizontalPodAutoscalers.ObjectMeta)
		if found {
			currentAnalysis.ParentObject = parent
		}
		a.Results = append(a.Results, currentAnalysis)
	}

	return a.Results, nil
}

type PodInfo interface {
	GetPodSpec() corev1.PodSpec
}

type DeploymentInfo struct {
	*appsv1.Deployment
}

func (d DeploymentInfo) GetPodSpec() corev1.PodSpec {
	return d.Spec.Template.Spec
}

// define a structure for ReplicationController
type ReplicationControllerInfo struct {
	*corev1.ReplicationController
}

func (rc ReplicationControllerInfo) GetPodSpec() corev1.PodSpec {
	return rc.Spec.Template.Spec
}

// define a structure for ReplicaSet
type ReplicaSetInfo struct {
	*appsv1.ReplicaSet
}

func (rs ReplicaSetInfo) GetPodSpec() corev1.PodSpec {
	return rs.Spec.Template.Spec
}

// define a structure for StatefulSet
type StatefulSetInfo struct {
	*appsv1.StatefulSet
}

// implement PodInfo for StatefulSetInfo
func (ss StatefulSetInfo) GetPodSpec() corev1.PodSpec {
	return ss.Spec.Template.Spec
}
