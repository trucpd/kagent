/*
Copyright 2025.

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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentTemplateSpec defines the desired state of AgentTemplate
type AgentTemplateSpec struct {
	// Description is a brief summary of the agent template.
	// +optional
	Description string `json:"description,omitempty"`

	// Author is the creator of the agent template.
	// +optional
	Author string `json:"author,omitempty"`

	// Tags are labels to categorize the agent template.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// AgentSpec is the agent specification that this template defines.
	AgentSpec AgentSpec `json:"agentSpec"`
}

// AgentTemplateStatus defines the observed state of AgentTemplate
type AgentTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AgentTemplate is the Schema for the agenttemplates API
type AgentTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentTemplateSpec   `json:"spec,omitempty"`
	Status AgentTemplateStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AgentTemplateList contains a list of AgentTemplate
type AgentTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentTemplate{}, &AgentTemplateList{})
}
