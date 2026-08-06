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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
)

func admissionTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add agents scheme: %v", err)
	}
	return s
}

func admittedCondition(pool *agentsv1alpha1.SandboxPool) *metav1.Condition {
	return apimeta.FindStatusCondition(pool.Status.Conditions, agentsv1alpha1.SandboxPoolConditionAdmitted)
}

func TestSetAdmissionCondition(t *testing.T) {
	tests := []struct {
		name        string
		desired     int32
		admission   plugins.PoolAdmission
		admErr      *domain.AppError
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{
			name:       "uncapped admission is granted",
			desired:    40,
			wantStatus: metav1.ConditionTrue,
			wantReason: agentsv1alpha1.SandboxPoolReasonAdmissionGranted,
		},
		{
			name:       "cap equal to desired is still a full grant",
			desired:    40,
			admission:  plugins.PoolAdmission{Admitted: ptr.To(int32(40))},
			wantStatus: metav1.ConditionTrue,
			wantReason: agentsv1alpha1.SandboxPoolReasonAdmissionGranted,
		},
		{
			name:    "cap below desired surfaces the plugin's own reason",
			desired: 40,
			admission: plugins.PoolAdmission{
				Admitted: ptr.To(int32(36)),
				Reason:   agentsv1alpha1.SandboxPoolReasonQuotaExhausted,
				Message:  "quota granted 4 of 8",
			},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  agentsv1alpha1.SandboxPoolReasonQuotaExhausted,
			wantMessage: "quota granted 4 of 8",
		},
		{
			name:       "a capacity error reads the same as a cap",
			desired:    40,
			admErr:     plugins.NewInsufficientResources("no quota left", nil),
			wantStatus: metav1.ConditionFalse,
			wantReason: agentsv1alpha1.SandboxPoolReasonQuotaExhausted,
		},
		{
			name:       "an invalid spec is called out separately",
			desired:    40,
			admErr:     plugins.NewInvalidSpec("missing quota url", nil),
			wantStatus: metav1.ConditionFalse,
			wantReason: agentsv1alpha1.SandboxPoolReasonAdmissionRejected,
		},
		{
			name:       "an unreachable backend is neither a cap nor a rejection",
			desired:    40,
			admErr:     plugins.NewInternal("scheduler unreachable", nil),
			wantStatus: metav1.ConditionFalse,
			wantReason: agentsv1alpha1.SandboxPoolReasonAdmissionError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &agentsv1alpha1.SandboxPoolStatus{}
			setAdmissionCondition(status, tt.desired, tt.admission, tt.admErr)

			cond := apimeta.FindStatusCondition(status.Conditions, agentsv1alpha1.SandboxPoolConditionAdmitted)
			if cond == nil {
				t.Fatal("Admitted condition was not set")
			}
			if cond.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", cond.Status, tt.wantStatus)
			}
			if cond.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", cond.Reason, tt.wantReason)
			}
			if tt.wantMessage != "" && cond.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", cond.Message, tt.wantMessage)
			}
			if cond.Message == "" {
				t.Error("Message must never be empty — operators read it from kubectl describe")
			}
		})
	}
}

// The regression this whole path exists for: a Pool whose admission cannot be
// satisfied used to abandon the reconcile before writing status, so it kept
// advertising the last state it managed to publish — healthy — for exactly as
// long as it was stuck.
func TestHandleAdmissionFailurePublishesObservedStatus(t *testing.T) {
	scheme := admissionTestScheme(t)
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec:       agentsv1alpha1.SandboxPoolSpec{Replicas: 40},
		Status: agentsv1alpha1.SandboxPoolStatus{
			// A stale snapshot from before the pods were lost.
			IdleReplicas: 40,
			Phase:        agentsv1alpha1.SandboxPoolPhaseReady,
		},
	}
	cli := newTestClientBuilder(t).WithObjects(pool).WithStatusSubresource(pool).Build()
	r := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

	// Only 32 pods actually survive.
	pods := make([]corev1.Pod, 0, 32)
	for i := range 32 {
		pods = append(pods, createTestPod("idle-"+string(rune('a'+i%26))+string(rune('0'+i/26)), agentsv1alpha1.SandboxPhaseIdle))
	}

	admErr := plugins.NewInsufficientResources("quota exhausted: request 8, free 0", nil)
	res, err := r.handleAdmissionFailure(context.Background(), pool, pods, plugins.PoolAdmission{}, admErr)
	if err != nil {
		t.Fatalf("a capacity shortfall must not surface as a reconcile error, got %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Error("a capacity shortfall must schedule a retry")
	}

	if pool.Status.IdleReplicas != 32 {
		t.Errorf("IdleReplicas = %d, want 32 — status must reflect reality, not the last successful cycle",
			pool.Status.IdleReplicas)
	}
	cond := admittedCondition(pool)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("Admitted condition must be False, got %+v", cond)
	}
	if cond.Reason != agentsv1alpha1.SandboxPoolReasonQuotaExhausted {
		t.Errorf("Reason = %q, want %q", cond.Reason, agentsv1alpha1.SandboxPoolReasonQuotaExhausted)
	}
}

func TestHandleAdmissionFailureClassifiesByKind(t *testing.T) {
	tests := []struct {
		name         string
		admErr       *domain.AppError
		admission    plugins.PoolAdmission
		wantErr      bool
		wantRequeue  bool
		requeueLower time.Duration
	}{
		{
			name:         "capacity defers using the plugin's cooldown",
			admErr:       plugins.NewInsufficientResources("full", nil),
			admission:    plugins.PoolAdmission{RetryAfter: 30 * time.Second},
			wantRequeue:  true,
			requeueLower: 30 * time.Second,
		},
		{
			name:        "capacity without a cooldown falls back to the default cadence",
			admErr:      plugins.NewInsufficientResources("full", nil),
			wantRequeue: true,
		},
		{
			// Retrying reproduces the identical rejection; only a spec change
			// can move this forward, and that re-triggers us anyway.
			name:   "an invalid spec parks without retrying",
			admErr: plugins.NewInvalidSpec("bad label", nil),
		},
		{
			name:    "a transient backend failure stays a reconcile error",
			admErr:  plugins.NewInternal("rpc timeout", nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := admissionTestScheme(t)
			pool := &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
				Spec:       agentsv1alpha1.SandboxPoolSpec{Replicas: 4},
			}
			cli := newTestClientBuilder(t).WithObjects(pool).WithStatusSubresource(pool).Build()
			r := &SandboxPoolReconciler{Client: cli, Scheme: scheme}

			res, err := r.handleAdmissionFailure(context.Background(), pool, nil, tt.admission, tt.admErr)

			if tt.wantErr && err == nil {
				t.Fatal("expected a reconcile error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected reconcile error: %v", err)
			}
			if tt.wantRequeue && res.RequeueAfter <= 0 {
				t.Error("expected a scheduled retry")
			}
			if !tt.wantRequeue && res.RequeueAfter != 0 {
				t.Errorf("expected no scheduled retry, got %s", res.RequeueAfter)
			}
			if tt.requeueLower > 0 && res.RequeueAfter <= tt.requeueLower {
				t.Errorf("RequeueAfter = %s, want strictly more than the plugin's %s cooldown",
					res.RequeueAfter, tt.requeueLower)
			}
		})
	}
}

func TestAdmissionRetryDelay(t *testing.T) {
	// The pad keeps the retry on the far side of the backend's own clock;
	// waking at exactly the reported deadline earns another rejection.
	got := admissionRetryDelay(plugins.PoolAdmission{RetryAfter: 10 * time.Second})
	if got != 10*time.Second+admissionRetrySkew {
		t.Errorf("RetryAfter = %s, want %s", got, 10*time.Second+admissionRetrySkew)
	}

	// With no plugin-supplied cooldown, fall back to the jittered base cadence
	// rather than inventing a fixed delay that would resynchronise the fleet.
	fallback := admissionRetryDelay(plugins.PoolAdmission{})
	if fallback <= 0 || fallback > 2*RequeueAfter {
		t.Errorf("fallback delay = %s, want a jittered value near %s", fallback, RequeueAfter)
	}
}
