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

package sandboxpool

import (
	"context"

	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxpool/autoscalingstate"
)

// syncAutoscaling drives one cycle of the per-Pool autoscaler decision
// pipeline: build a Snapshot via the configured Loader, run the pure
// autoscalingstate.Decide function, Commit the accumulated writes.
//
// Returns only an error — the parent Reconcile method always picks the
// outer ctrl.Result. The autoscaler does not currently produce a
// shorter requeue hint than the existing reconcile cadence; the
// proactive trigger's IdleThresholdSeconds (default 30 s) is short
// enough that idle-zero candidates get re-evaluated naturally.
//
// Errors propagate to the parent reconcile so controller-runtime's
// normal back-off kicks in.
func (r *SandboxPoolReconciler) syncAutoscaling(ctx context.Context, pool *agentsv1alpha1.SandboxPool) error {
	if r.AutoscalingLoader == nil {
		// Autoscaler not wired (unit tests, legacy deployments). Skip.
		return nil
	}

	snap, err := r.AutoscalingLoader.Load(ctx, pool)
	if err != nil {
		klog.ErrorS(err, "autoscaler: snapshot load failed; will retry",
			"namespace", pool.Namespace, "name", pool.Name)
		return err
	}

	mut := autoscalingstate.NewMutator(snap)
	autoscalingstate.Decide(snap, mut)
	if !mut.HasWrites() {
		return nil
	}

	if err := mut.Commit(ctx, r.Client, r.commitEventRecorder()); err != nil {
		klog.ErrorS(err, "autoscaler: commit failed; will retry",
			"namespace", pool.Namespace, "name", pool.Name)
		return err
	}
	return nil
}

// commitEventRecorder returns the autoscaler's event sink. Returns nil
// when no recorder has been wired (unit tests) — Commit treats that
// case as "drop events silently".
//
// The interface is k8s.io/client-go/tools/events.EventRecorder (new
// API). controller-runtime's mgr.GetEventRecorder satisfies it; the
// legacy mgr.GetEventRecorderFor returns the incompatible
// record.EventRecorder shape and is no longer used here.
func (r *SandboxPoolReconciler) commitEventRecorder() events.EventRecorder {
	return r.AutoscalingEventRecorder
}
