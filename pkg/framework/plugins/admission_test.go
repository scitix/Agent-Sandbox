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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// admissionPlugin returns a fixed admission (and optional error) so the
// manager's folding rules can be exercised without a real backend.
type admissionPlugin struct {
	BasePlugin
	name  string
	adm   PoolAdmission
	err   *domain.AppError
	calls int
}

func (p *admissionPlugin) Name() string { return p.name }

func (p *admissionPlugin) PreUpdatePool(_ context.Context, _ *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (PoolAdmission, *domain.AppError) {
	p.calls++
	return p.adm, p.err
}

func TestPoolAdmissionZeroValueIsUnconstrained(t *testing.T) {
	var adm PoolAdmission
	if adm.Capped() {
		t.Error("zero PoolAdmission must not be capped")
	}
	if got := adm.AdmittedOr(40); got != 40 {
		t.Errorf("AdmittedOr(40) = %d, want 40 (uncapped)", got)
	}
	if adm.Updated || adm.RetryAfter != 0 {
		t.Errorf("zero PoolAdmission must be inert, got %+v", adm)
	}
}

func TestPoolAdmissionAdmittedOrHonoursCap(t *testing.T) {
	adm := PoolAdmission{Admitted: ptr.To(int32(6))}
	if !adm.Capped() {
		t.Error("admission with Admitted set must report Capped")
	}
	if got := adm.AdmittedOr(40); got != 6 {
		t.Errorf("AdmittedOr(40) = %d, want 6", got)
	}
}

// The manager must fold several plugins into the single most conservative
// answer: the tightest cap, the longest wait, and the explanation belonging to
// whichever plugin imposed that cap.
func TestPluginManagerPreUpdatePoolMergesAdmissions(t *testing.T) {
	loose := &admissionPlugin{
		name: "loose",
		adm: PoolAdmission{
			Admitted:   ptr.To(int32(30)),
			RetryAfter: 5 * time.Second,
			Reason:     "LooseCap",
			Message:    "loose plugin capped at 30",
		},
	}
	tight := &admissionPlugin{
		name: "tight",
		adm: PoolAdmission{
			Updated:    true,
			Admitted:   ptr.To(int32(12)),
			RetryAfter: 90 * time.Second,
			Reason:     "TightCap",
			Message:    "tight plugin capped at 12",
		},
	}

	pm := NewPluginManager(loose, tight)
	adm, err := pm.PreUpdatePool(context.Background(), &agentsv1alpha1.SandboxPool{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := adm.AdmittedOr(40); got != 12 {
		t.Errorf("Admitted = %d, want 12 (smallest cap wins)", got)
	}
	if adm.RetryAfter != 90*time.Second {
		t.Errorf("RetryAfter = %s, want 90s (longest wait wins)", adm.RetryAfter)
	}
	if !adm.Updated {
		t.Error("Updated must be true when any plugin mutated the pool")
	}
	if adm.Reason != "TightCap" || adm.Message != "tight plugin capped at 12" {
		t.Errorf("Reason/Message must come from the binding cap, got %q / %q", adm.Reason, adm.Message)
	}
}

// Merge order must not matter — the tightest cap wins regardless of which
// plugin ran first.
func TestPluginManagerPreUpdatePoolMergeIsOrderIndependent(t *testing.T) {
	mk := func() (*admissionPlugin, *admissionPlugin) {
		return &admissionPlugin{name: "a", adm: PoolAdmission{Admitted: ptr.To(int32(7)), Reason: "A"}},
			&admissionPlugin{name: "b", adm: PoolAdmission{Admitted: ptr.To(int32(20)), Reason: "B"}}
	}

	a1, b1 := mk()
	forward, _ := NewPluginManager(a1, b1).PreUpdatePool(context.Background(), &agentsv1alpha1.SandboxPool{}, nil)
	a2, b2 := mk()
	reverse, _ := NewPluginManager(b2, a2).PreUpdatePool(context.Background(), &agentsv1alpha1.SandboxPool{}, nil)

	if forward.AdmittedOr(50) != 7 || reverse.AdmittedOr(50) != 7 {
		t.Errorf("cap must be order-independent: forward=%d reverse=%d",
			forward.AdmittedOr(50), reverse.AdmittedOr(50))
	}
	if forward.Reason != "A" || reverse.Reason != "A" {
		t.Errorf("reason must follow the binding cap: forward=%q reverse=%q", forward.Reason, reverse.Reason)
	}
}

// A plugin that mutated the pool before a later plugin failed must still have
// its mutation reported, otherwise the caller drops a change it already made.
func TestPluginManagerPreUpdatePoolKeepsUpdatedOnError(t *testing.T) {
	mutator := &admissionPlugin{name: "mutator", adm: PoolAdmission{Updated: true}}
	failer := &admissionPlugin{name: "failer", err: NewInternal("backend down", nil)}
	never := &admissionPlugin{name: "never"}

	pm := NewPluginManager(mutator, failer, never)
	adm, err := pm.PreUpdatePool(context.Background(), &agentsv1alpha1.SandboxPool{}, nil)

	if err == nil {
		t.Fatal("expected the failing plugin's error to propagate")
	}
	if !adm.Updated {
		t.Error("Updated from the earlier plugin must survive a later plugin's error")
	}
	if never.calls != 0 {
		t.Errorf("plugins after a failure must not run, got %d calls", never.calls)
	}
}

func TestPluginManagerPreUpdatePoolNilManagerAdmitsEverything(t *testing.T) {
	var pm *PluginManager
	adm, err := pm.PreUpdatePool(context.Background(), &agentsv1alpha1.SandboxPool{}, nil)
	if err != nil {
		t.Fatalf("nil manager must not error, got %v", err)
	}
	if adm.Capped() {
		t.Error("nil manager must not cap the admission")
	}
}

// kindedDetail is a structured payload that classifies itself, mirroring what
// a scheduler plugin attaches when it needs Detail for its own response body.
type kindedDetail struct{ kind PluginErrorKind }

func (d kindedDetail) Kind() PluginErrorKind { return d.kind }

func TestKindFromAppErrorReadsKindedDetail(t *testing.T) {
	tests := []struct {
		name string
		err  *domain.AppError
		want PluginErrorKind
	}{
		{
			// Without KindedDetail this would fall through to the 500 → Internal
			// heuristic and lose the plugin's own classification.
			name: "structured detail overrides the status heuristic",
			err: &domain.AppError{
				Code:   domain.ErrCodeInternal,
				Detail: kindedDetail{kind: PluginErrKindInsufficientResources},
			},
			want: PluginErrKindInsufficientResources,
		},
		{
			name: "empty kind falls back to the status heuristic",
			err: &domain.AppError{
				Code:   domain.ErrCodeTooManyRequests,
				Detail: kindedDetail{kind: ""},
			},
			want: PluginErrKindInsufficientResources,
		},
		{
			name: "a bare PluginErrorKind detail still wins",
			err: &domain.AppError{
				Code:   domain.ErrCodeInternal,
				Detail: PluginErrKindInvalidSpec,
			},
			want: PluginErrKindInvalidSpec,
		},
		{
			name: "an unclassified detail leaves the heuristic in charge",
			err: &domain.AppError{
				Code:   domain.ErrCodeInternal,
				Detail: struct{ Foo string }{Foo: "bar"},
			},
			want: PluginErrKindInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KindFromAppError(tt.err); got != tt.want {
				t.Errorf("KindFromAppError() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A plugin that admits the call but caps below the probed target has said the
// target does not fit; the search must narrow instead of accepting it.
func TestProbeAcceptedReplicasTreatsCapAsInsufficient(t *testing.T) {
	capped := &admissionPlugin{
		name: "capped",
		adm:  PoolAdmission{Admitted: ptr.To(int32(3)), Message: "quota holds 3"},
	}
	res := ProbeAcceptedReplicas(context.Background(), NewPluginManager(capped),
		&agentsv1alpha1.SandboxPool{}, nil, 0, 10)

	if res.Kind != ProbeInsufficientResources {
		t.Errorf("Kind = %v, want ProbeInsufficientResources", res.Kind)
	}
	// A probe at or below the cap admits, so the search converges on the cap
	// itself rather than giving up at current.
	if res.Accepted != 3 {
		t.Errorf("Accepted = %d, want 3 (the plugin's own cap)", res.Accepted)
	}
	if res.Err == nil || res.Err.Message != "quota holds 3" {
		t.Errorf("the plugin's own message must survive into the probe result, got %v", res.Err)
	}
}
