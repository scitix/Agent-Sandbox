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

package quota

import (
	"context"
	"testing"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// policyProvider is a Provider that implements the optional TemplatePolicy.
// Defined here rather than pulled from quotatest so this package's own test
// stays free of test-support packages that import it.
type policyProvider struct{ billed bool }

func (policyProvider) Enabled() bool { return true }

func (policyProvider) ListForUser(context.Context, string, string) ([]Info, *domain.AppError) {
	return nil, nil
}

func (policyProvider) DeriveShortName(string) string { return "" }

func (p policyProvider) RequiresQuota(map[string]string) bool { return p.billed }

// TestSizingFor covers the ways a caller can get an answer out of the optional
// interface — and, in the last two cases, out of its absence.
//
// "No policy" and "policy says no" are DIFFERENT answers on purpose, and
// conflating them is the mistake this test exists to catch: the first leaves
// the caller's choice intact (the open-source build, where an InstanceType
// catalog sizes Pools with no quota anywhere), while the second is a
// deployment deliberately saying "not this one".
func TestSizingFor(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     PoolSizing
	}{
		{
			name:     "provider with a policy, template billed",
			provider: policyProvider{billed: true},
			want:     PoolSizingBilled,
		},
		{
			name:     "provider with a policy, template not billed",
			provider: policyProvider{billed: false},
			want:     PoolSizingFreeForm,
		},
		{
			// Noop is the open-source Provider and deliberately does NOT
			// implement TemplatePolicy: the absence of the interface is what
			// keeps "either" reachable.
			name:     "provider without the optional interface",
			provider: NewNoop(),
			want:     PoolSizingEither,
		},
		{
			name:     "nil provider",
			provider: nil,
			want:     PoolSizingEither,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SizingFor(tc.provider, map[string]string{"anything": "true"}); got != tc.want {
				t.Errorf("SizingFor = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNoopHasNoTemplatePolicy pins the design decision above: if Noop ever
// grows the method, this fails and the comment explaining that "no interface =
// nothing is billed" has to be revisited.
func TestNoopHasNoTemplatePolicy(t *testing.T) {
	if _, ok := any(NewNoop()).(TemplatePolicy); ok {
		t.Error("Noop must not implement TemplatePolicy: the open-source answer is the missing interface")
	}
}
