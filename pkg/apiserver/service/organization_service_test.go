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
	"sort"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// buildOrgScheme returns a scheme with all types required by the organization service.
func buildOrgScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = agentsv1alpha1.AddToScheme(s)
	return s
}

// newOrgKeyStore creates a fake SecretKeyStore populated with the given API key secrets.
func newOrgKeyStore(t *testing.T, scheme *runtime.Scheme, secrets ...*corev1.Secret) *apikey.SecretKeyStore {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(scheme)
	for _, s := range secrets {
		b = b.WithObjects(s)
	}
	return apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           b.Build(),
		SecretsNamespace: "agentbox-system",
	})
}

// makeAPIKeySecret builds a minimal API key Secret with the given user/team labels.
func makeAPIKeySecret(name, user, team string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentbox-apikey-" + name,
			Namespace: "agentbox-system",
			Labels: map[string]string{
				apikey.LabelType: apikey.LabelTypeValue,
				apikey.LabelUser: user,
				apikey.LabelTeam: team,
			},
		},
		Data: map[string][]byte{
			"token":     []byte("deadbeef"),
			"role":      []byte("tenant"),
			"user":      []byte(user),
			"team":      []byte(team),
			"namespace": []byte(""),
			"issuedAt":  []byte(time.Now().UTC().Format(time.RFC3339)),
		},
	}
}

// makeNamespace creates a minimal Kubernetes Namespace.
func makeNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// TestListTeams_ReturnsUniqueTeams verifies that ListTeams returns all unique team names
// derived from API Key secrets, deduplicated and sorted.
func TestListTeams_ReturnsUniqueTeams(t *testing.T) {
	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme,
		makeAPIKeySecret("aa", "alice", "team1"),
		makeAPIKeySecret("bb", "alice", "team1"),    // same team, different key
		makeAPIKeySecret("cc", "bob", "team1"),      // same team
		makeAPIKeySecret("dd", "carol", "research"), // different team
		makeAPIKeySecret("ee", "dave", "infra"),     // another team
	)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc := NewOrganizationService(c, ks)

	teams, appErr := svc.ListTeams(context.Background())
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}

	// Expect 3 unique teams: team1, infra, research (sorted)
	if len(teams) != 3 {
		t.Fatalf("expected 3 unique teams, got %d: %v", len(teams), teams)
	}
	if !sort.StringsAreSorted(teams) {
		t.Errorf("expected teams to be sorted, got: %v", teams)
	}
	teamSet := make(map[string]bool, len(teams))
	for _, team := range teams {
		teamSet[team] = true
	}
	for _, expected := range []string{"team1", "infra", "research"} {
		if !teamSet[expected] {
			t.Errorf("expected team %q in result, got %v", expected, teams)
		}
	}
}

// TestListTeams_NoKeysReturnsEmpty verifies that ListTeams returns an empty
// list when no API Key secrets exist.
func TestListTeams_NoKeysReturnsEmpty(t *testing.T) {
	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc := NewOrganizationService(c, ks)

	teams, appErr := svc.ListTeams(context.Background())
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(teams) != 0 {
		t.Fatalf("expected 0 teams, got %d: %v", len(teams), teams)
	}
}

// TestListUsersByTeam_ReturnsMatchingUsers verifies that ListUsersByTeam returns
// all unique users belonging to the given team.
func TestListUsersByTeam_ReturnsMatchingUsers(t *testing.T) {
	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme,
		makeAPIKeySecret("aa", "alice", "team1"),
		makeAPIKeySecret("bb", "alice", "team1"), // same user, deduped
		makeAPIKeySecret("cc", "bob", "team1"),
		makeAPIKeySecret("dd", "carol", "research"), // different team, excluded
	)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc := NewOrganizationService(c, ks)

	users, appErr := svc.ListUsersByTeam(context.Background(), "team1")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}

	if len(users) != 2 {
		t.Fatalf("expected 2 users for team team1, got %d: %v", len(users), users)
	}
	if !sort.StringsAreSorted(users) {
		t.Errorf("expected users to be sorted, got: %v", users)
	}
	userSet := make(map[string]bool, len(users))
	for _, u := range users {
		userSet[u] = true
	}
	for _, expected := range []string{"alice", "bob"} {
		if !userSet[expected] {
			t.Errorf("expected user %q in result, got %v", expected, users)
		}
	}
	if userSet["carol"] {
		t.Errorf("unexpected user carol in team1 team result")
	}
}

// TestListUsersByTeam_NoMatch verifies that ListUsersByTeam returns an empty list
// when no users belong to the given team.
func TestListUsersByTeam_NoMatch(t *testing.T) {
	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme,
		makeAPIKeySecret("aa", "alice", "team1"),
	)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc := NewOrganizationService(c, ks)

	users, appErr := svc.ListUsersByTeam(context.Background(), "nonexistent-team")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 users, got %d: %v", len(users), users)
	}
}

// TestListUsersByTeam_EmptyTeamReturnsError verifies that ListUsersByTeam returns an
// error when the team parameter is empty.
func TestListUsersByTeam_EmptyTeamReturnsError(t *testing.T) {
	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc := NewOrganizationService(c, ks)

	_, appErr := svc.ListUsersByTeam(context.Background(), "")
	if appErr == nil {
		t.Fatal("expected error for empty team, got nil")
	}
}

// TestListNamespaces_ReturnsAllNamespaces verifies that ListNamespaces returns
// all Kubernetes namespace names, sorted.
func TestListNamespaces_ReturnsAllNamespaces(t *testing.T) {
	ns1 := makeNamespace("default")
	ns2 := makeNamespace("kube-system")
	ns3 := makeNamespace("agentbox-system")
	ns4 := makeNamespace("production")

	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns1, ns2, ns3, ns4).Build()
	svc := NewOrganizationService(c, ks)

	namespaces, appErr := svc.ListNamespaces(context.Background())
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(namespaces) != 4 {
		t.Fatalf("expected 4 namespaces, got %d: %v", len(namespaces), namespaces)
	}
	if !sort.StringsAreSorted(namespaces) {
		t.Errorf("expected namespaces to be sorted, got: %v", namespaces)
	}
	nsSet := make(map[string]bool, len(namespaces))
	for _, n := range namespaces {
		nsSet[n] = true
	}
	for _, expected := range []string{"default", "kube-system", "agentbox-system", "production"} {
		if !nsSet[expected] {
			t.Errorf("expected namespace %q in result, got %v", expected, namespaces)
		}
	}
}

// TestListNamespaces_EmptyCluster verifies that ListNamespaces returns an empty list
// when no namespaces exist.
func TestListNamespaces_EmptyCluster(t *testing.T) {
	scheme := buildOrgScheme(t)
	ks := newOrgKeyStore(t, scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc := NewOrganizationService(c, ks)

	namespaces, appErr := svc.ListNamespaces(context.Background())
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(namespaces) != 0 {
		t.Fatalf("expected 0 namespaces, got %d: %v", len(namespaces), namespaces)
	}
}
