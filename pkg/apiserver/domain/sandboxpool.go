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

package domain

import (
	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// PodDiagnosticEvent is a single Kubernetes Warning event for a pod.
type PodDiagnosticEvent struct {
	// Reason is the event reason, e.g. "Failed", "BackOff", "ErrImagePull".
	Reason string
	// Message is the human-readable event message.
	Message string
	// LastTimestamp is the RFC3339 time of the most recent occurrence.
	LastTimestamp string
	// Count is the total number of times this event fired.
	Count int32
}

// PodDiagnostic holds status detail for a single problematic pod.
// For List responses only Reason/Message (from Pod YAML) are populated.
// For Get responses Events (from K8s Events API) are also populated.
type PodDiagnostic struct {
	// PodName is the name of the pod.
	PodName string
	// Phase is the agentbox pod phase (e.g. "starting", "failed").
	Phase string
	// Reason is a machine-readable cause, e.g. "ImagePullBackOff", "OOMKilled".
	Reason string
	// Message is a human-readable description.
	Message string
	// Events contains Warning events (only populated for Get, not List).
	Events []PodDiagnosticEvent
}

// SandboxPool is the domain model for a SandboxPool CRD.
type SandboxPool struct {
	Name      string
	Namespace string
	Spec      agentsv1alpha1.SandboxPoolSpec
	Status    agentsv1alpha1.SandboxPoolStatus
	// Overrides stores persisted pool-level overrides that are re-applied during template sync.
	Overrides *PoolTemplateOverrides
	// CPU is the sum of all containers' CPU requests (raw K8s string, e.g. "8000m").
	CPU string
	// Memory is the sum of all containers' memory requests (raw K8s string, e.g. "8Gi").
	Memory string
	// PodDiagnostics holds real-time diagnostic info for Starting/Failed pods.
	// Populated on List (Pod YAML only) and Get (Pod YAML + Events).
	PodDiagnostics []PodDiagnostic
	// Team is the team label of the pool owner (from CRD label).
	Team string
	// User is the user label of the pool owner (from CRD label).
	User string
	// TemplateVersion is the version of the source SandboxTemplate at last sync (from annotation).
	TemplateVersion string
	// CreatedAt is the RFC3339 creation time of the pool (from metadata.creationTimestamp).
	CreatedAt string
	// PoolDocs is the Markdown pool-specific usage docs. Inside the domain layer this
	// field carries the raw template from the linked SandboxTemplate's
	// agentbox.navix.sh/pool-docs annotation. The handler layer substitutes
	// ${poolName}, ${clusterId}, ${apiKey} before serialising to the HTTP response.
	PoolDocs string
}

// CreateSandboxPoolInput carries all parameters needed to create a new SandboxPool.
type CreateSandboxPoolInput struct {
	Name            string
	Namespace       string
	TemplateName    string // references a cluster-scoped SandboxTemplate (optional)
	Labels          map[string]string
	Annotations     map[string]string
	Spec            agentsv1alpha1.SandboxPoolSpec
	Team            string                 // from auth.Team, propagated to pod label
	User            string                 // from auth.User, propagated to pod label
	Overrides       *PoolTemplateOverrides // nil = no overrides
	ImagePullSecret *ImagePullSecretInput  // nil = no pull secret to materialise
}

// ImagePullSecretInput carries registry credentials attached by the caller at pool creation.
// Not persisted in domain form; the service layer immediately materialises it into a
// Kubernetes Secret (kubernetes.io/dockerconfigjson) with an OwnerReference to the pool.
type ImagePullSecretInput struct {
	Registries []RegistryCredential
}

// RegistryCredential is a single registry auth entry.
type RegistryCredential struct {
	Registry string
	Username string
	Password string
}

// UpdateSandboxPoolInput carries parameters for updating an existing SandboxPool.
type UpdateSandboxPoolInput struct {
	Name                   string
	Namespace              string
	Replicas               *int32                                 // nil = don't modify
	MinReplicas            *int32                                 // nil = don't modify
	MaxReplicas            *int32                                 // nil = don't modify
	PodCreationImagePolicy *agentsv1alpha1.PodCreationImagePolicy // nil = don't modify
	OverrideImage          string                                 // empty = don't modify
	Autoscaling            *agentsv1alpha1.PoolAutoscalingSpec    // nil = don't modify
}

// DeleteSandboxPoolResult is returned after a SandboxPool is deleted.
type DeleteSandboxPoolResult struct {
	Name      string
	Namespace string
}

// SyncSandboxPoolTemplateInput carries parameters for syncing a pool's spec from its source template.
type SyncSandboxPoolTemplateInput struct {
	Name      string
	Namespace string
}

// PoolTemplateOverrides holds per-pool overrides applied on top of the referenced template.
// Applied in the service layer AFTER copying EmbeddedSandboxTemplate from the source template.
// The effective computed values are stored in spec, while the override intent is persisted
// in pool annotations so SyncTemplate can re-apply it against newer template versions.
type PoolTemplateOverrides struct {
	// Image overrides containers[0].Image; empty = no-op.
	Image string `json:"image,omitempty"`
	// ResourceMultiplier uniformly scales all container CPU and memory requests+limits,
	// and all reservation.replicaQuota values. Must be >= 1; 1 = no change.
	ResourceMultiplier int32 `json:"resourceMultiplier,omitempty"`
	// ImagePullSecretName is the deterministic Secret name injected into
	// spec.template.spec.imagePullSecrets; empty = no-op.
	ImagePullSecretName string `json:"imagePullSecretName,omitempty"`
}

// SyncTemplatePreviewResult is the result of a dry-run SyncTemplate operation.
type SyncTemplatePreviewResult struct {
	// SpecYaml is the EmbeddedSandboxTemplate YAML after applying all overrides.
	SpecYaml string
	// Version is the version of the source template.
	Version string
}
