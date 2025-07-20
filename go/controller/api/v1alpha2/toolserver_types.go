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
	"database/sql/driver"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ToolServerSpec defines the desired state of ToolServer.
type ToolServerSpec struct {
	Description string           `json:"description"`
	Config      ToolServerConfig `json:"config"`
}

type ToolServerProtocol string

const (
	ToolServerProtocolSse            ToolServerProtocol = "sse"
	ToolServerProtocolStreamableHttp ToolServerProtocol = "streamableHttp"
)

// Only one of sse or streamableHttp must be specified
type ToolServerConfig struct {
	// Default protocol is streamableHttp
	// +kubebuilder:validation:Enum=sse;streamableHttp
	Protocol ToolServerProtocol `json:"protocol"`
	// URL of the tool server
	URL string `json:"url"`
	// List of headers to send with the request
	// +optional
	HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
	// +optional
	SseReadTimeout *metav1.Duration `json:"sseReadTimeout,omitempty"`
	// Only applicable for streamableHttp
	// +optional
	TerminateOnClose bool `json:"terminateOnClose,omitempty"`
}

func (t *ToolServerConfig) Scan(value interface{}) error {
	return json.Unmarshal(value.([]byte), t)
}

func (t ToolServerConfig) Value() (driver.Value, error) {
	return json.Marshal(t)
}

type ValueSourceType string

const (
	ConfigMapValueSource ValueSourceType = "ConfigMap"
	SecretValueSource    ValueSourceType = "Secret"
)

// ValueSource defines a source for configuration values from a Secret or ConfigMap
type ValueSource struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Type ValueSourceType `json:"type"`
	// The reference to the ConfigMap or Secret. Can either be a reference to a resource in the same namespace,
	// or a reference to a resource in a different namespace in the form "namespace/name".
	// If namespace is not provided, the default namespace is used.
	ValueRef string `json:"valueRef"`
	Key      string `json:"key"`
}

// ValueRef represents a configuration value
// Only one of value or valueFrom must be specified
// +kubebuilder:validation:XValidation:rule="(has(self.value) && !has(self.valueFrom)) || (!has(self.value) && has(self.valueFrom))",message="Exactly one of value or valueFrom must be specified"
type ValueRef struct {
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

// ToolServerStatus defines the observed state of ToolServer.
type ToolServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration int64              `json:"observedGeneration"`
	Conditions         []metav1.Condition `json:"conditions"`
	// +kubebuilder:validation:Optional
	DiscoveredTools []*MCPTool `json:"discoveredTools"`
}

type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ts

// ToolServer is the Schema for the toolservers API.
type ToolServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ToolServerSpec   `json:"spec,omitempty"`
	Status ToolServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ToolServerList contains a list of ToolServer.
type ToolServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ToolServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ToolServer{}, &ToolServerList{})
}
