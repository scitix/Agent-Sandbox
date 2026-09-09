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

// The bug this exists to prevent: one person's two credentials resolving to
// different namespaces, so what one creates the other cannot see.
//
// A namespace is a PER-CLUSTER fact and a credential is used across clusters,
// so nothing a credential carries about namespaces can be right everywhere.
// Each cluster therefore answers this itself, from team+user alone — and the
// reason it is pinned rather than left to review is that getting it wrong has
// no symptom: listing a namespace that does not exist is a legal EMPTY result,
// not an error, so both sides return 200 and nobody is told anything.
func TestResolveNamespace(t *testing.T) {
	cases := []struct {
		name       string
		namespaces []string
		want       string
	}{
		{
			name:       "a tenant with their own namespace gets it",
			namespaces: []string{"default", "t-acme-bob"},
			want:       "t-acme-bob",
		},
		{
			name:       "a tenant this cluster has no namespace for shares the default",
			namespaces: []string{"default"},
			want:       "default",
		},
		{
			name: "another tenant's namespace is not mistaken for this one's",
			// The convention is the whole lookup, so a near-miss must miss.
			namespaces: []string{"default", "t-acme-bobby", "t-acme2-bob"},
			want:       "default",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iam := iamWith(t, tc.namespaces...)
			got, err := iam.ResolveNamespace(context.Background(), "acme", "bob")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The answer must not depend on which credential asked. Nothing here passes a
// credential at all, which is the guarantee stated as a test: there is no
// parameter through which one could differ from another.
func TestTheAnswerDoesNotDependOnWhoAsks(t *testing.T) {
	iam := iamWith(t, "default", "t-acme-bob")
	first, _ := iam.ResolveNamespace(context.Background(), "acme", "bob")
	second, _ := iam.ResolveNamespace(context.Background(), "acme", "bob")
	if first != second || first != "t-acme-bob" {
		t.Fatalf("same person, different answers: %q vs %q", first, second)
	}
}

// Never empty. An empty namespace does not mean "no namespace" to the
// Kubernetes client — it means EVERY namespace, so a caller that fell through
// to it would list the whole cluster rather than nothing.
func TestResolveNeverReturnsEmpty(t *testing.T) {
	iam := iamWith(t, "default")
	for _, p := range [][2]string{{"", ""}, {"acme", ""}, {"", "bob"}} {
		got, _ := iam.ResolveNamespace(context.Background(), p[0], p[1])
		if got == "" {
			t.Fatalf("team=%q user=%q resolved to the empty namespace", p[0], p[1])
		}
	}
}
