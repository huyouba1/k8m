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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type NetworkPolicyAnalyzer struct{}

func (NetworkPolicyAnalyzer) Analyze(a common.Analyzer) ([]common.Result, error) {

	kind := "NetworkPolicy"
	apiDoc := kubernetes.K8sApiReference{
		Kind: kind,
		ApiVersion: schema.GroupVersion{
			Group:   "networking",
			Version: "v1",
		},
		OpenapiSchema: a.OpenapiSchema,
	}

	AnalyzerErrorsMetric.DeletePartialMatch(map[string]string{
		"analyzer_name": kind,
	})

	// get all network policies in the namespace

	var policies []*networkingv1.NetworkPolicy
	err := kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&networkingv1.NetworkPolicy{}).WithLabelSelector(a.LabelSelector).Namespace(a.Namespace).List(&policies).Error
	if err != nil {
		return nil, err
	}

	var preAnalysis = map[string]common.PreAnalysis{}

	for _, policy := range policies {
		var failures []common.Failure

		// Check if policy allows traffic to all pods in the namespace
		if len(policy.Spec.PodSelector.MatchLabels) == 0 {
			doc := apiDoc.GetApiDocV2("spec.podSelector.matchLabels")

			failures = append(failures, common.Failure{
				Text:          fmt.Sprintf("Network policy allows traffic to all pods: %s", policy.Name),
				KubernetesDoc: doc,
				Sensitive: []common.Sensitive{
					{
						Unmasked: policy.Name,
						Masked:   util.MaskString(policy.Name),
					},
				},
			})
		} else {
			// Check if policy is not applied to any pods

			var pods []*corev1.Pod
			err = kom.Cluster(a.ClusterID).WithContext(a.Context).Resource(&corev1.Pod{}).WithLabelSelector(util.MapToString(policy.Spec.PodSelector.MatchLabels)).Namespace(a.Namespace).List(&pods).Error

			if err != nil {
				return nil, err
			}
			if len(pods) == 0 {
				failures = append(failures, common.Failure{
					Text: fmt.Sprintf("Network policy is not applied to any pods: %s", policy.Name),
					Sensitive: []common.Sensitive{
						{
							Unmasked: policy.Name,
							Masked:   util.MaskString(policy.Name),
						},
					},
				})
			}
		}

		if len(failures) > 0 {
			preAnalysis[fmt.Sprintf("%s/%s", policy.Namespace, policy.Name)] = common.PreAnalysis{
				FailureDetails: failures,
				NetworkPolicy:  *policy,
			}
			AnalyzerErrorsMetric.WithLabelValues(kind, policy.Name, policy.Namespace).Set(float64(len(failures)))

		}
	}

	for key, value := range preAnalysis {
		currentAnalysis := common.Result{
			Kind:  kind,
			Name:  key,
			Error: value.FailureDetails,
		}
		a.Results = append(a.Results, currentAnalysis)
	}

	return a.Results, nil
}
