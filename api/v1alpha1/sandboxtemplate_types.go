// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TemplateVisibilityRule describes a single visibility rule.
// Team and Users are combined with AND semantics: both must match (empty = wildcard).
// Multiple Rules in a TemplateVisibility are combined with OR semantics.
type TemplateVisibilityRule struct {
	// Team specifies the team that can see the template.
	// Empty means any team.
	// +optional
	Team string `json:"team,omitempty"`

	// Users specifies the users that can see the template.
	// Empty means any user.
	// +optional
	Users []string `json:"users,omitempty"`
}

// TemplateVisibility controls the visibility of a SandboxTemplate.
// Rules are evaluated with OR semantics: a caller is visible if it matches any rule.
// An empty Rules list means the template is public (visible to all).
type TemplateVisibility struct {
	// Rules is the list of visibility rules.
	// +optional
	Rules []TemplateVisibilityRule `json:"rules,omitempty"`
}

// SandboxTemplateSpec defines the desired state of SandboxTemplate
type SandboxTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	EmbeddedSandboxTemplate `json:",inline"`

	// Version is an optional semantic version string for this template (e.g. "v1.2.0").
	// +optional
	Version string `json:"version,omitempty"`

	// Description is a human-readable description of this template.
	// +optional
	Description string `json:"description,omitempty"`

	// Visibility controls which tenants can see this template.
	// When nil or Rules is empty, the template is public (visible to all).
	// +optional
	Visibility *TemplateVisibility `json:"visibility,omitempty"`
}

type EmbeddedSandboxTemplate struct {
	// Template defines the Pod template. ALL Pods in this Pool share the same
	// resources (requests/limits). The image specified here is used as the IDLE image
	// unless IdleImage is explicitly set.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Template corev1.PodTemplateSpec `json:"template,omitempty"`

	// IdleImage is the image to use when Pods are in the idle state.
	// If not specified, the image from Template.Spec.Containers[0].Image will be used.
	// +optional
	IdleImage string `json:"idleImage,omitempty"`

	// Runtimes specifies the runtimes to use for the sandbox pods. Each runtime has a type and optional configuration.
	// If not specified, a default runtime will be used.
	// +listType=map
	// +listMapKey=name
	// +optional
	Runtimes []SandboxRuntimeSpec `json:"runtimes,omitempty"`
}

// SandboxReservationSpec holds SI Scheduler integration settings for each sandbox pod.
type SandboxReservationSpec struct {
	PriorityClassName string              `json:"priorityClassName,omitempty"`
	ReplicaQuota      corev1.ResourceList `json:"replicaQuota"`
}

type SandboxRuntimeSpec struct {
	// Name specifies the name of the runtime to use for the sandbox pods.
	// Supported values are "e2b", "swerex", "aiosanbdox", etc.
	Name string `json:"name"`

	// Port specifies the port number that the runtime should listen on for incoming connections.
	// +optional
	Port *int32 `json:"port,omitempty"`

	// Protocol for port. Must be UDP, TCP, or SCTP.
	// Defaults to "TCP".
	// +optional
	// +default="TCP"
	Protocol *corev1.Protocol `json:"protocol,omitempty"`

	// Description is a human-readable description of this runtime.
	// +optional
	Description string `json:"description,omitempty"`

	// LogDir is the path to the runtime's log file inside the container.
	// When set, the GetLogs API can retrieve runtime logs via file read.
	// Example: "/tmp/envd.log"
	// +optional
	LogDir string `json:"logDir,omitempty"`

	// ReadinessProbe defines the readiness check configuration for the runtime.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// Config contains runtime-specific configuration parameters.
	// The content and structure of this field depend on the runtime type.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +optional
	Config *runtime.RawExtension `json:"config,omitempty"`
}

// SandboxTemplateStatus defines the observed state of SandboxTemplate.
type SandboxTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the SandboxTemplate resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=sbt
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SandboxTemplate is the Schema for the sandboxtemplates API
type SandboxTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SandboxTemplate
	// +required
	Spec SandboxTemplateSpec `json:"spec"`

	// status defines the observed state of SandboxTemplate
	// +optional
	Status SandboxTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SandboxTemplateList contains a list of SandboxTemplate
type SandboxTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SandboxTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SandboxTemplate{}, &SandboxTemplateList{})
}
