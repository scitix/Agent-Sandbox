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

package service

import (
	"time"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool/poststarthooks"
)

// CreateSandboxInput carries all parameters needed to create a new sandbox.
// The shape is service-internal: it holds parsed timeouts (durations), the
// cluster prefix already split out of the pool name, and post-start hooks
// (which are not part of the native wire spec — they are injected by the
// E2B compatibility handler).
type CreateSandboxInput struct {
	ClusterID       string // target cluster ID parsed from pool name prefix; empty means local
	PoolName        string
	Namespace       string
	Image           string
	ContainerImages map[string]string
	Labels          map[string]string
	Annotations     map[string]string
	Metadata        map[string]string
	StartupTimeout  time.Duration // 0 means no timeout
	IdleTimeout     time.Duration // 0 means no expiry
	// PostStartHooks are actions to run after the sandbox transitions Starting → Running.
	// Serialized to a pod annotation at claim time; consumed by the controller.
	PostStartHooks []poststarthooks.Action
}

// CreateSandboxPoolInput carries all parameters needed to create a new SandboxPool.
// Service-internal: Spec is the fully resolved (template-merged) CRD spec; Team/User
// come from the authenticated caller.
type CreateSandboxPoolInput struct {
	Name            string
	Namespace       string
	TemplateName    string // references a cluster-scoped SandboxTemplate (optional)
	Labels          map[string]string
	Annotations     map[string]string
	Spec            agentsv1alpha1.SandboxPoolSpec
	Team            string                     // from auth.Team, propagated to pod label
	User            string                     // from auth.User, propagated to pod label
	Overrides       *gen.PoolTemplateOverrides // nil = no overrides
	ImagePullSecret *gen.ImagePullSecretInput  // nil = no pull secret to materialise
}

// UpdateSandboxPoolInput carries parameters for updating an existing SandboxPool.
// Service-internal because Autoscaling and PodCreationImagePolicy are typed against
// the CRD package rather than the gen wire shape.
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
