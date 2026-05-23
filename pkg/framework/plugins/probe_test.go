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
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// capPlugin admits any PreUpdatePool whose newPool.Spec.Replicas <= cap.
// Above the cap it returns NewInsufficientResources, mimicking the closed-
// source Scitix scheduler running out of reservation headroom.
type capPlugin struct {
	BasePlugin
	cap   int32
	calls int // counts probe attempts
}

func (p *capPlugin) Name() string { return "cap" }

func (p *capPlugin) PreUpdatePool(_ context.Context, newPool *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	p.calls++
	if newPool.Spec.Replicas <= p.cap {
		return false, nil
	}
	return false, NewInsufficientResources("over cap", nil)
}

// internalErrPlugin always returns an internal-classified error. Used to
// verify ProbeAcceptedReplicas aborts the binary search rather than
// classifying the failure as saturation.
type internalErrPlugin struct {
	BasePlugin
}

func (p *internalErrPlugin) Name() string { return "internal" }
func (p *internalErrPlugin) PreUpdatePool(_ context.Context, _ *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	return false, NewInternal("rpc broken", errors.New("dial tcp: i/o timeout"))
}

// invalidSpecPlugin rejects anything as InvalidSpec.
type invalidSpecPlugin struct {
	BasePlugin
}

func (p *invalidSpecPlugin) Name() string { return "invalid" }
func (p *invalidSpecPlugin) PreUpdatePool(_ context.Context, _ *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	return false, NewInvalidSpec("missing required label", nil)
}

func makePool() *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 0},
	}
}

func TestProbeAcceptedReplicas_NoOpWhenCandidateAtOrBelowCurrent(t *testing.T) {
	pm := NewPluginManager(&capPlugin{cap: 100})
	res := ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, 50, 50)
	if res.Kind != ProbeOK || res.Accepted != 50 {
		t.Errorf("expected ProbeOK{50}, got %+v", res)
	}
	res = ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, 50, 40)
	if res.Kind != ProbeOK || res.Accepted != 50 {
		t.Errorf("candidate < current — expected ProbeOK{current}, got %+v", res)
	}
}

func TestProbeAcceptedReplicas_FullAdmit(t *testing.T) {
	plug := &capPlugin{cap: 100}
	pm := NewPluginManager(plug)
	res := ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, 50, 80)
	if res.Kind != ProbeOK || res.Accepted != 80 {
		t.Errorf("expected ProbeOK{80}, got %+v", res)
	}
	if plug.calls != 1 {
		t.Errorf("expected exactly 1 probe call on full admit, got %d", plug.calls)
	}
}

func TestProbeAcceptedReplicas_BinarySearchOnInsufficient(t *testing.T) {
	tests := []struct {
		name      string
		cap       int32
		current   int32
		candidate int32
		want      int32
	}{
		{"cap = 60, range 50..80 → 60", 60, 50, 80, 60},
		{"cap = 50, no headroom — accepted stays at current", 50, 50, 80, 50},
		{"cap = 51, single-step headroom", 51, 50, 80, 51},
		{"cap = 79, just below candidate", 79, 50, 80, 79},
		{"cap = 100 ≥ candidate — full admit", 100, 50, 80, 80},
		{"big range, cap mid", 64, 0, 128, 64},
		{"big range, cap at 1", 1, 0, 128, 1},
		{"cap < current — accepted stays at current", 5, 50, 80, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plug := &capPlugin{cap: tt.cap}
			pm := NewPluginManager(plug)
			res := ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, tt.current, tt.candidate)
			if res.Accepted != tt.want {
				t.Errorf("Accepted = %d, want %d (calls=%d)", res.Accepted, tt.want, plug.calls)
			}
			// When tt.cap >= candidate, the optimistic first probe wins → ProbeOK.
			// Otherwise we entered the binary-search branch and exit with
			// ProbeInsufficientResources, even when Accepted == current.
			if tt.cap >= tt.candidate {
				if res.Kind != ProbeOK {
					t.Errorf("Kind = %v, want ProbeOK", res.Kind)
				}
			} else if res.Kind != ProbeInsufficientResources {
				t.Errorf("Kind = %v, want ProbeInsufficientResources", res.Kind)
			}
			// Probe-count sanity: ≤ log2(candidate-current)+1 (+1 for the
			// optimistic outer probe). Keep this loose to tolerate rounding.
			delta := tt.candidate - tt.current
			maxCalls := 2
			for d := int32(1); d < delta; d *= 2 {
				maxCalls++
			}
			if plug.calls > maxCalls {
				t.Errorf("probe calls = %d, expected ≤ %d", plug.calls, maxCalls)
			}
		})
	}
}

func TestProbeAcceptedReplicas_AbortOnInternalError(t *testing.T) {
	pm := NewPluginManager(&internalErrPlugin{})
	res := ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, 10, 80)
	if res.Kind != ProbeInternalError {
		t.Errorf("Kind = %v, want ProbeInternalError", res.Kind)
	}
	if res.Accepted != 10 {
		t.Errorf("Accepted = %d, want current(10)", res.Accepted)
	}
	if res.Err == nil {
		t.Error("expected diagnostic Err to be populated")
	}
}

func TestProbeAcceptedReplicas_AbortOnInvalidSpec(t *testing.T) {
	pm := NewPluginManager(&invalidSpecPlugin{})
	res := ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, 10, 80)
	if res.Kind != ProbeInvalidSpec {
		t.Errorf("Kind = %v, want ProbeInvalidSpec", res.Kind)
	}
	if res.Accepted != 10 {
		t.Errorf("Accepted = %d, want current(10)", res.Accepted)
	}
}

func TestProbeAcceptedReplicas_InternalErrorMidBinarySearch(t *testing.T) {
	// First probe (the optimistic candidate) returns InsufficientResources;
	// mid-search probe pivots to Internal. Expectation: search stops, kind
	// becomes Internal, Accepted reflects the last successful admit (if any).
	var calls int
	pm := NewPluginManager(&pivotPlugin{
		probe: func(target int32) *domain.AppError {
			calls++
			switch calls {
			case 1:
				return NewInsufficientResources("over cap", nil) // forces binary search
			default:
				return NewInternal("rpc broken", nil)
			}
		},
	})
	res := ProbeAcceptedReplicas(context.Background(), pm, makePool(), nil, 0, 16)
	if res.Kind != ProbeInternalError {
		t.Errorf("Kind = %v, want ProbeInternalError", res.Kind)
	}
}

// pivotPlugin runs probe(target) on each PreUpdatePool. Used to drive
// scenarios where the response shape changes across probes.
type pivotPlugin struct {
	BasePlugin
	probe func(target int32) *domain.AppError
}

func (p *pivotPlugin) Name() string { return "pivot" }
func (p *pivotPlugin) PreUpdatePool(_ context.Context, newPool *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	return false, p.probe(newPool.Spec.Replicas)
}

func TestProbeAcceptedReplicas_NilPluginManagerAdmitsAlways(t *testing.T) {
	res := ProbeAcceptedReplicas(context.Background(), nil, makePool(), nil, 0, 50)
	if res.Kind != ProbeOK || res.Accepted != 50 {
		t.Errorf("nil PluginManager: got %+v, want ProbeOK{50}", res)
	}
}
