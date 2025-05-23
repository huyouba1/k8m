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

package common

import (
	"context"
	"time"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	"github.com/huyouba1/k8m/pkg/ai"
	regv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autov2 "k8s.io/api/autoscaling/v2"
	v1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	gtwapi "sigs.k8s.io/gateway-api/apis/v1"
)

type IAnalyzer interface {
	Analyze(analysis Analyzer) ([]Result, error)
}

type Analyzer struct {
	ClusterID     string
	Context       context.Context
	Namespace     string
	LabelSelector string
	AIClient      ai.IAI
	PreAnalysis   map[string]PreAnalysis
	Results       []Result
	OpenapiSchema *openapi_v2.Document
}

type PreAnalysis struct {
	Pod                      v1.Pod
	FailureDetails           []Failure
	Deployment               appsv1.Deployment
	ReplicaSet               appsv1.ReplicaSet
	PersistentVolumeClaim    v1.PersistentVolumeClaim
	Endpoint                 v1.Endpoints
	Ingress                  networkv1.Ingress
	HorizontalPodAutoscalers autov2.HorizontalPodAutoscaler
	PodDisruptionBudget      policyv1.PodDisruptionBudget
	StatefulSet              appsv1.StatefulSet
	NetworkPolicy            networkv1.NetworkPolicy
	Node                     v1.Node
	ValidatingWebhook        regv1.ValidatingWebhookConfiguration
	MutatingWebhook          regv1.MutatingWebhookConfiguration
	GatewayClass             gtwapi.GatewayClass
	Gateway                  gtwapi.Gateway
	HTTPRoute                gtwapi.HTTPRoute
}

type Result struct {
	Kind         string    `json:"kind"`
	Name         string    `json:"name"`
	Error        []Failure `json:"error"`
	Details      string    `json:"details"`
	ParentObject string    `json:"parentObject"`
}

type AnalysisStats struct {
	Analyzer     string        `json:"analyzer"`
	DurationTime time.Duration `json:"durationTime"`
}

type Failure struct {
	Text          string
	KubernetesDoc string
	Sensitive     []Sensitive
}

type Sensitive struct {
	Unmasked string
	Masked   string
}
