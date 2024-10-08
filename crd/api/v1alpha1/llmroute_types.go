/*
Copyright 2024.

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LLMRouteSpec defines the desired state of LLMRoute.
type LLMRouteSpec struct {
	// ParentRefs references the resources (usually Gateways) that a Route wants to be attached to.
	ParentRefs []corev1.ObjectReference `json:"parentRefs,omitempty"`
	// BackendRefs defines the backend(s) where matching requests should be sent.
	// Default is a k8s service.
	BackendRefs []corev1.ObjectReference `json:"backendRefs,omitempty"`
	Model       Model                    `json:"model,omitempty"`
}

type Model struct {
	Name string `json:"name,omitempty"`
}

// LLMRouteStatus defines the observed state of LLMRoute
type LLMRouteStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// LLMRoute is the Schema for the LLMRoutes API
type LLMRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LLMRouteSpec   `json:"spec,omitempty"`
	Status LLMRouteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LLMRouteList contains a list of LLMRoute
type LLMRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMRoute `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMRoute{}, &LLMRouteList{})
}
