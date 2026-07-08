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

	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool/poststarthooks"
)

// MetaKeyScalingGroup is the reserved create-metadata key that pins a sandbox
// to a specific autoscaling group (e.g. "1c2Gi"). Both the E2B-compatible and
// the native create handlers consume it into CreateSandboxInput.RequestedScalingGroup
// and strip it from the metadata that gets stored on the pod. Empty / absent =
// no group constraint (any member pool of the env is eligible — unchanged
// behaviour). The value is matched verbatim against a member's
// EnvClusterMemberConfig.ScalingGroup.
const MetaKeyScalingGroup = "agentbox.scitix.ai/scaling-group"

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
	// RequestedScalingGroup, when non-empty, hard-scopes pool selection to the
	// env's member pools whose autoscaling group
	// (EnvClusterMemberConfig.ScalingGroup) equals this value. If no member of
	// the env belongs to that group the create fails (503) rather than falling
	// back to another group. Empty = no constraint. Set from the reserved
	// metadata key MetaKeyScalingGroup by the create handlers.
	RequestedScalingGroup string
	// PostStartHooks are actions to run after the sandbox transitions Starting → Running.
	// Serialized to a pod annotation at claim time; consumed by the controller.
	PostStartHooks []poststarthooks.Action
}
