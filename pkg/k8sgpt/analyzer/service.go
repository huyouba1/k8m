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
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ServiceAnalyzer struct{}

func (ServiceAnalyzer) Analyze(a common.Analyzer) ([]common.Result, error) {

	kind := "Service"
	apiDoc := kubernetes.K8sApiReference{
		Kind: kind,
		ApiVersion: schema.GroupVersion{
			Group:   "",
			Version: "v1",
		},
		OpenapiSchema: a.OpenapiSchema,
	}

	AnalyzerErrorsMetric.DeletePartialMatch(map[string]string{
		"analyzer_name": kind,
	})

	// search all namespaces for pods that are not running
	var list []*corev1.Endpoints
	err := kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&corev1.Endpoints{}).WithLabelSelector(a.LabelSelector).Namespace(a.Namespace).List(&list).Error

	if err != nil {
		return nil, err
	}

	var preAnalysis = map[string]common.PreAnalysis{}

	for _, ep := range list {
		var failures []common.Failure

		// Check for empty service
		if len(ep.Subsets) == 0 {
			if _, ok := ep.Annotations[resourcelock.LeaderElectionRecordAnnotationKey]; ok {
				continue
			}

			var svc *corev1.Service
			err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&corev1.Service{}).Namespace(ep.Namespace).Name(ep.Name).Get(&svc).Error
			if err != nil {
				klog.V(6).Infof("Service %s/%s does not exist", ep.Namespace, ep.Name)
				continue
			}

			for k, v := range svc.Spec.Selector {
				doc := apiDoc.GetApiDocV2("spec.selector")

				failures = append(failures, common.Failure{
					Text:          fmt.Sprintf("Service has no endpoints, expected label %s=%s", k, v),
					KubernetesDoc: doc,
					Sensitive: []common.Sensitive{
						{
							Unmasked: k,
							Masked:   util.MaskString(k),
						},
						{
							Unmasked: v,
							Masked:   util.MaskString(v),
						},
					},
				})
			}
		} else {
			count := 0
			pods := []string{}

			// Check through container status to check for crashes
			for _, epSubset := range ep.Subsets {
				apiDoc.Kind = "Endpoints"

				if len(epSubset.NotReadyAddresses) > 0 {
					for _, addresses := range epSubset.NotReadyAddresses {
						count++
						pods = append(pods, addresses.TargetRef.Kind+"/"+addresses.TargetRef.Name)
					}
				}
			}

			if count > 0 {
				doc := apiDoc.GetApiDocV2("subsets.notReadyAddresses")

				failures = append(failures, common.Failure{
					Text:          fmt.Sprintf("Service has not ready endpoints, pods: %s, expected %d", pods, count),
					KubernetesDoc: doc,
					Sensitive:     []common.Sensitive{},
				})
			}
		}
		// fetch event
		var events []*eventsv1.Event
		err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&eventsv1.Event{}).WithLabelSelector("involvedObject.name=" + ep.Name).Namespace(a.Namespace).List(&events).Error

		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if event.Type != "Normal" {
				failures = append(failures, common.Failure{
					Text: fmt.Sprintf("Service %s/%s has event %s", ep.Namespace, ep.Name, event.Note),
				})
			}
		}
		if len(failures) > 0 {
			preAnalysis[fmt.Sprintf("%s/%s", ep.Namespace, ep.Name)] = common.PreAnalysis{
				Endpoint:       *ep,
				FailureDetails: failures,
			}
			AnalyzerErrorsMetric.WithLabelValues(kind, ep.Name, ep.Namespace).Set(float64(len(failures)))
		}
	}

	for key, value := range preAnalysis {
		var currentAnalysis = common.Result{
			Kind:  kind,
			Name:  key,
			Error: value.FailureDetails,
		}

		parent, found := util.GetParent(a.Context, a.ClusterID, value.Endpoint.ObjectMeta)
		if found {
			currentAnalysis.ParentObject = parent
		}
		a.Results = append(a.Results, currentAnalysis)
	}
	return a.Results, nil
}
