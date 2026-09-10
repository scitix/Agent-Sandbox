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
	"net/http"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

func poolOwnedBy(team, user string) *agentsv1alpha1.SandboxPool {
	labels := map[string]string{}
	if team != "" {
		labels[agentsv1alpha1.LabelTeam] = team
	}
	if user != "" {
		labels[agentsv1alpha1.LabelUser] = user
	}
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "p", Labels: labels},
	}
}

func callerInput(team, user, role string) CreateSandboxInput {
	return CreateSandboxInput{
		Role: role,
		Labels: map[string]string{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		},
	}
}

func TestAssertPoolOwnedByCaller(t *testing.T) {
	cases := []struct {
		name    string
		pool    *agentsv1alpha1.SandboxPool
		input   CreateSandboxInput
		wantErr bool
	}{
		{
			name:  "same tenant is allowed",
			pool:  poolOwnedBy("k8s", "ylli"),
			input: callerInput("k8s", "ylli", ""),
		},
		{
			// The reported case: one tenant claiming a warm Pod out of another
			// tenant's pre-paid pool.
			name:    "different user in the same team is refused",
			pool:    poolOwnedBy("k8s", "ylli"),
			input:   callerInput("k8s", "someone-else", ""),
			wantErr: true,
		},
		{
			name:    "different team is refused",
			pool:    poolOwnedBy("k8s", "ylli"),
			input:   callerInput("ai-infra", "xyjiang02", ""),
			wantErr: true,
		},
		{
			name:  "admin may act across tenants",
			pool:  poolOwnedBy("k8s", "ylli"),
			input: callerInput("ai-infra", "xyjiang02", apikey.RoleAdmin),
		},
		{
			// Pools that pre-date tenant labelling cannot express the rule;
			// refusing them would break existing deployments.
			name:  "unlabelled pool is allowed",
			pool:  poolOwnedBy("", ""),
			input: callerInput("ai-infra", "xyjiang02", ""),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertPoolOwnedByCaller(tc.pool, tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected the create to be refused")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected the create to be allowed, got %v", err)
			}
			if tc.wantErr && err.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", err.Code)
			}
		})
	}
}
