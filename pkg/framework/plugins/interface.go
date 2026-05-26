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

package plugins

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

// Plugin defines lifecycle hooks for SandboxPool operations.
// Implement only the hooks you need; embed BasePlugin for no-op defaults.
type Plugin interface {
	// Name returns a unique identifier for this plugin (used in logs).
	Name() string

	// Start is called once during bootstrap, after the host has constructed
	// every plugin but before the reconciler begins processing. Plugins that
	// need their own informers (e.g. a ConfigMap-backed catalog) register
	// them here via the supplied framework.Handle.Cache(). A non-nil error
	// aborts program startup.
	Start(ctx context.Context, h framework.Handle) error

	// PreCreatePool is called after input validation and template resolution,
	// before the SandboxPool is persisted to Kubernetes. The plugin may:
	//   - Mutate pool.ObjectMeta / pool.Spec
	//   - Read from input for context (auth info, caller-supplied metadata)
	//   - Reject by returning an error (ideally *AdmissionError with status hint)
	//
	// Return updated=true when the plugin mutated pool. Callers are expected
	// to verify the mutation with equality.Semantic.DeepEqual against a
	// pre-call snapshot before persisting, so spurious updated=true is safe
	// (just wasteful) but missed updated=true silently loses the mutation.
	PreCreatePool(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (updated bool, err *domain.AppError)

	// PreUpdatePool is called before the SandboxPool update is persisted.
	// newPool is the state that will be written; pods is the current Pod list
	// for context. The plugin may mutate newPool or reject the operation.
	// Return updated=true if newPool was mutated and must be persisted.
	PreUpdatePool(ctx context.Context, newPool *agentsv1alpha1.SandboxPool, pods []corev1.Pod) (updated bool, err *domain.AppError)

	// PreDeletePool is called before the SandboxPool is deleted from Kubernetes.
	// The plugin may reject the operation. Mutation is rarely meaningful here
	// (the object is about to be deleted); updated=true is reserved for the
	// niche case where a plugin needs to set a finalizer or annotation
	// before the delete proceeds.
	PreDeletePool(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (updated bool, err *domain.AppError)

	// PreCreatePod is called after the Pod object is fully assembled but BEFORE
	// it is submitted to Kubernetes. Plugins may mutate pod.Spec (e.g. inject
	// NodeAffinity). A non-nil error aborts pod creation for this attempt;
	// the reconciler will retry on the next tick. Return updated=true when
	// the plugin mutated pod.
	PreCreatePod(ctx context.Context, pod *corev1.Pod, pool *agentsv1alpha1.SandboxPool) (updated bool, err *domain.AppError)
}

// ---------------------------------------------------------------------------
// BasePlugin — embed for no-op defaults
// ---------------------------------------------------------------------------

// BasePlugin provides no-op implementations for all Plugin hooks.
// Embed it in your plugin struct and override only the hooks you need.
type BasePlugin struct{}

func (BasePlugin) Name() string { return "base" }

var _ Plugin = (*BasePlugin)(nil)

func (BasePlugin) Start(_ context.Context, _ framework.Handle) error { return nil }

func (BasePlugin) PreCreatePool(_ context.Context, _ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	return false, nil
}
func (BasePlugin) PreUpdatePool(_ context.Context, _ *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	return false, nil
}
func (BasePlugin) PreDeletePool(_ context.Context, _ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	return false, nil
}
func (BasePlugin) PreCreatePod(_ context.Context, _ *corev1.Pod, _ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	return false, nil
}
