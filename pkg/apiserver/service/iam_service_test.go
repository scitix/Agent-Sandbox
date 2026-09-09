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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// iamWith builds an IAM service over a cluster that has exactly `namespaces`.
func iamWith(t *testing.T, namespaces ...string) IAMService {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	objs := make([]client.Object, 0, len(namespaces))
	for _, n := range namespaces {
		objs = append(objs, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: n}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return NewIAMService(c)
}

// The bug this exists to prevent: a credential minted on one cluster records
// the namespace THAT cluster resolved, and is then used against a cluster that
// maps the same tenant elsewhere. Listing the recorded namespace is a legal
// empty result rather than an error, so the person sees an empty console while
// their agent — holding a credential that records nothing — reads and writes
// real objects in the namespace this cluster actually uses. Nothing reports a
// problem anywhere, which is why this is pinned rather than left to review.
func TestEffectiveNamespace(t *testing.T) {
	cases := []struct {
		name       string
		namespaces []string
		recorded   string
		want       string
	}{
		{
			name:       "a recorded namespace that exists here is honoured",
			namespaces: []string{"default", "t-acme-bob"},
			recorded:   "t-acme-bob",
			want:       "t-acme-bob",
		},
		{
			name: "a deliberate pin to some other existing namespace is kept",
			// Not every namespace follows the t-<team>-<user> convention, and
			// a credential aimed at one on purpose must not be dragged back to
			// the convention.
			namespaces: []string{"default", "shared-stress"},
			recorded:   "shared-stress",
			want:       "shared-stress",
		},
		{
			name:       "a recorded namespace this cluster does not have is re-resolved",
			namespaces: []string{"default", "t-acme-bob"},
			recorded:   "t-other-bob",
			want:       "t-acme-bob",
		},
		{
			name:       "recording nothing resolves by convention",
			namespaces: []string{"default", "t-acme-bob"},
			recorded:   "",
			want:       "t-acme-bob",
		},
		{
			name:       "with no tenant namespace at all, everyone shares default",
			namespaces: []string{"default"},
			recorded:   "t-acme-bob",
			want:       "default",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iam := iamWith(t, tc.namespaces...)
			got := iam.EffectiveNamespace(context.Background(), tc.recorded, "acme", "bob")
			if got != tc.want {
				t.Fatalf("recorded %q -> %q, want %q", tc.recorded, got, tc.want)
			}
		})
	}
}

// Two credentials for the same person must agree, whatever either one records.
// They disagreeing is the whole bug: one creates, the other cannot see it.
func TestBothCredentialsForOnePersonAgree(t *testing.T) {
	iam := iamWith(t, "default", "t-acme-bob")
	fromOldKey := iam.EffectiveNamespace(context.Background(), "t-stale-bob", "acme", "bob")
	fromHubKey := iam.EffectiveNamespace(context.Background(), "", "acme", "bob")
	if fromOldKey != fromHubKey {
		t.Fatalf("same person, different namespaces: %q vs %q", fromOldKey, fromHubKey)
	}
}
