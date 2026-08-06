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
	"time"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

// PoolAdmission is the structured outcome of the PreUpdatePool hook.
//
// Its zero value means "fully admitted, nothing to retry" and is exactly
// equivalent to the plain (updated=false, err=nil) result other hooks return.
// Plugins that only mutate the Pool need to set nothing but Updated.
//
// The type exists because "the scheduler granted me 6 of the 8 replicas I
// asked for" is neither success nor failure: reporting it as an error forces
// callers to abandon the whole reconcile over a condition that is both
// expected and partially satisfiable, while reporting it as plain success
// silently over-creates Pods the backing quota cannot hold.
type PoolAdmission struct {
	// Updated reports whether the plugin mutated the Pool. Semantics are
	// identical to the bool the other hooks return: callers snapshot the
	// input first and persist only when Updated is true AND a semantic
	// comparison confirms an actual change.
	Updated bool

	// Admitted, when non-nil, caps how many replicas the caller may
	// materialise during THIS reconcile cycle. nil means no cap — the caller
	// converges towards pool.Spec.Replicas as usual.
	//
	// It is emphatically NOT a desired replica count. Spec.Replicas is owned
	// by the SandboxEnv / autoscaler; a plugin must never rewrite it. Admitted
	// throttles a single cycle and is recomputed from scratch on the next one,
	// so a Pool whose backing quota frees up converges without anyone editing
	// the spec.
	Admitted *int32

	// RetryAfter asks the caller to re-reconcile after this delay. Zero
	// leaves the pacing to the caller's own default. Backends that impose a
	// submit cooldown report the remaining wait here so the caller stops
	// hot-looping into a rejection it already knows is coming.
	RetryAfter time.Duration

	// Reason is a machine-readable CamelCase token suitable for a Condition
	// reason (e.g. "ResourceQuotaExhausted"). Empty when the admission was
	// unconstrained.
	Reason string

	// Message is the human-readable explanation surfaced on the Pool's
	// Condition. It should carry the numbers an operator needs to act —
	// which quota, which resource, how much was requested versus available.
	Message string
}

// Capped reports whether the admission limits this cycle's replica count.
func (a PoolAdmission) Capped() bool { return a.Admitted != nil }

// AdmittedOr returns the admitted replica cap, or full when uncapped.
func (a PoolAdmission) AdmittedOr(full int32) int32 {
	if a.Admitted == nil {
		return full
	}
	return *a.Admitted
}

// mergeInto folds other into a, applying the aggregation rules used when
// several plugins each return an admission for the same Pool:
//
//   - Updated   — logical OR; any plugin's mutation must be persisted.
//   - Admitted  — the smallest non-nil cap wins; the most conservative
//     plugin decides how far the caller may go.
//   - RetryAfter — the longest wait wins; retrying sooner than the slowest
//     backend allows only reproduces its rejection.
//   - Reason/Message — taken from whichever plugin supplied the winning
//     (smallest) cap, so the surfaced Condition explains the binding
//     constraint rather than an incidental one.
func (a PoolAdmission) mergeInto(other PoolAdmission) PoolAdmission {
	merged := a
	if other.Updated {
		merged.Updated = true
	}
	if other.RetryAfter > merged.RetryAfter {
		merged.RetryAfter = other.RetryAfter
	}
	if other.Admitted != nil && (merged.Admitted == nil || *other.Admitted < *merged.Admitted) {
		merged.Admitted = other.Admitted
		merged.Reason = other.Reason
		merged.Message = other.Message
	}
	return merged
}

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
	// for context. The plugin may mutate newPool, throttle it, or reject it.
	//
	// The returned PoolAdmission carries three separable outcomes: whether
	// newPool was mutated (Updated), how many replicas the caller may
	// materialise this cycle (Admitted), and how long to wait before trying
	// again (RetryAfter). Its zero value is the unconstrained "yes".
	//
	// Reserve the error return for admissions that cannot be satisfied at any
	// size — a malformed spec, an unreachable backend. Capacity shortfalls
	// belong in Admitted, so the caller can make partial progress instead of
	// abandoning the cycle.
	PreUpdatePool(ctx context.Context, newPool *agentsv1alpha1.SandboxPool, pods []corev1.Pod) (admission PoolAdmission, err *domain.AppError)

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
func (BasePlugin) PreUpdatePool(_ context.Context, _ *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (PoolAdmission, *domain.AppError) {
	return PoolAdmission{}, nil
}
func (BasePlugin) PreDeletePool(_ context.Context, _ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	return false, nil
}
func (BasePlugin) PreCreatePod(_ context.Context, _ *corev1.Pod, _ *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	return false, nil
}
