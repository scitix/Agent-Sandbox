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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const testTemplateVersion = "1.0.1"

func newTestSandboxTemplateService(t *testing.T, objs ...any) SandboxTemplateService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	for _, o := range objs {
		if v, ok := o.(*agentsv1alpha1.SandboxTemplate); ok {
			cb = cb.WithObjects(v)
		}
	}
	return NewSandboxTemplateService(cb.Build())
}

func makeSandboxTemplate(name, version string) *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     version,
			Description: "Test template " + name,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.36",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "sandbox", Image: "busybox:1.36"},
						},
					},
				},
			},
		},
	}
}

// adminAuth returns an AuthInfo representing an admin caller.
var adminAuth = domain.AuthInfo{Role: "admin", Team: "ops", User: "alice"}

// tenantAuth returns an AuthInfo representing a regular tenant caller.
func tenantAuth(team, user string) domain.AuthInfo {
	return domain.AuthInfo{Role: "tenant", Team: team, User: user}
}

func TestSandboxTemplateService_List_Empty(t *testing.T) {
	svc := newTestSandboxTemplateService(t)

	items, appErr := svc.List(context.Background(), adminAuth, true)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty list, got %d items", len(items))
	}
}

func TestSandboxTemplateService_List_MultipleTemplates(t *testing.T) {
	tmpl1 := makeSandboxTemplate("tmpl-a", "v1.0.0")
	tmpl2 := makeSandboxTemplate("tmpl-b", "v2.0.0")
	svc := newTestSandboxTemplateService(t, tmpl1, tmpl2)

	items, appErr := svc.List(context.Background(), adminAuth, true)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	names := map[string]bool{}
	for _, item := range items {
		names[item.Name] = true
	}
	if !names["tmpl-a"] {
		t.Fatal("expected tmpl-a in results")
	}
	if !names["tmpl-b"] {
		t.Fatal("expected tmpl-b in results")
	}
}

func TestSandboxTemplateService_Get_NotFound(t *testing.T) {
	svc := newTestSandboxTemplateService(t)

	_, appErr := svc.Get(context.Background(), "nonexistent")
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func TestSandboxTemplateService_Get_Success(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-a", "v1.2.3")
	tmpl.Spec.Description = "A useful template"
	svc := newTestSandboxTemplateService(t, tmpl)

	result, appErr := svc.Get(context.Background(), "tmpl-a")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Name != "tmpl-a" {
		t.Fatalf("expected name tmpl-a, got %s", result.Name)
	}
	if result.Version != "v1.2.3" {
		t.Fatalf("expected version v1.2.3, got %s", result.Version)
	}
	if result.Description != "A useful template" {
		t.Fatalf("expected description 'A useful template', got %s", result.Description)
	}
	if !strings.Contains(result.CrdYaml, "busybox:1.36") {
		t.Fatalf("expected crdYaml to contain busybox:1.36, got: %s", result.CrdYaml)
	}
}

func TestSandboxTemplateService_Create_Success(t *testing.T) {
	svc := newTestSandboxTemplateService(t)

	input := agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "new-template",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     "v0.1.0",
			Description: "New template",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "alpine:3.18",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "sandbox", Image: "alpine:3.18"},
						},
					},
				},
			},
		},
	}

	result, appErr := svc.Create(context.Background(), &input)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Name != "new-template" {
		t.Fatalf("expected name new-template, got %s", result.Name)
	}
	if result.Version != "v0.1.0" {
		t.Fatalf("expected version v0.1.0, got %s", result.Version)
	}

	// Verify it can be retrieved
	fetched, appErr := svc.Get(context.Background(), "new-template")
	if appErr != nil {
		t.Fatalf("get after create: %v", appErr)
	}
	if !strings.Contains(fetched.CrdYaml, "alpine:3.18") {
		t.Fatalf("expected crdYaml to contain alpine:3.18, got: %s", fetched.CrdYaml)
	}
}

func TestSandboxTemplateService_Create_Duplicate(t *testing.T) {
	tmpl := makeSandboxTemplate("existing", "v1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	input := agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "existing",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.36",
			},
		},
	}

	_, appErr := svc.Create(context.Background(), &input)
	if appErr == nil {
		t.Fatal("expected error for duplicate, got nil")
	}
	if appErr.Code != domain.ErrCodeConflict {
		t.Fatalf("expected ErrCodeConflict, got %d", appErr.Code)
	}
}

func TestSandboxTemplateService_Delete_NotFound(t *testing.T) {
	svc := newTestSandboxTemplateService(t)

	appErr := svc.Delete(context.Background(), "nonexistent")
	if appErr == nil {
		t.Fatal("expected error, got nil")
	}
	if appErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", appErr.Code)
	}
}

func TestSandboxTemplateService_Delete_Success(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-to-delete", "v1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	appErr := svc.Delete(context.Background(), "tmpl-to-delete")
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}

	// Verify it's gone
	_, getErr := svc.Get(context.Background(), "tmpl-to-delete")
	if getErr == nil {
		t.Fatal("expected not-found error after delete, got nil")
	}
	if getErr.Code != domain.ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound after delete, got %d", getErr.Code)
	}
}

// ---------------------------------------------------------------------------
// Visibility tests
// ---------------------------------------------------------------------------

func makeTemplateWithVisibility(name string, rules []agentsv1alpha1.TemplateVisibilityRule) *agentsv1alpha1.SandboxTemplate {
	tmpl := makeSandboxTemplate(name, "v1.0.0")
	if rules != nil {
		tmpl.Spec.Visibility = &agentsv1alpha1.TemplateVisibility{Rules: rules}
	}
	return tmpl
}

func TestVisibility_PublicTemplateVisibleToAll(t *testing.T) {
	// nil Visibility → public
	tmpl := makeSandboxTemplate("public-tmpl", "v1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	for _, auth := range []domain.AuthInfo{
		tenantAuth("teamA", "alice"),
		tenantAuth("teamB", "bob"),
		tenantAuth("", ""),
	} {
		items, appErr := svc.List(context.Background(), auth, false)
		if appErr != nil {
			t.Fatalf("unexpected error: %v", appErr)
		}
		if len(items) != 1 {
			t.Errorf("auth=%+v: expected 1 item (public), got %d", auth, len(items))
		}
	}
}

func TestVisibility_EmptyRulesIsPublic(t *testing.T) {
	tmpl := makeTemplateWithVisibility("empty-rules", []agentsv1alpha1.TemplateVisibilityRule{})
	svc := newTestSandboxTemplateService(t, tmpl)

	items, appErr := svc.List(context.Background(), tenantAuth("anyTeam", "anyUser"), false)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for empty rules (public), got %d", len(items))
	}
}

func TestVisibility_TeamRule(t *testing.T) {
	tmpl := makeTemplateWithVisibility("team-tmpl", []agentsv1alpha1.TemplateVisibilityRule{
		{Team: "teamA"},
	})
	svc := newTestSandboxTemplateService(t, tmpl)

	// teamA can see it
	items, appErr := svc.List(context.Background(), tenantAuth("teamA", "alice"), false)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 {
		t.Errorf("teamA: expected 1 item, got %d", len(items))
	}

	// teamB cannot see it
	items, appErr = svc.List(context.Background(), tenantAuth("teamB", "bob"), false)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 0 {
		t.Errorf("teamB: expected 0 items, got %d", len(items))
	}
}

func TestVisibility_UsersRule(t *testing.T) {
	tmpl := makeTemplateWithVisibility("user-tmpl", []agentsv1alpha1.TemplateVisibilityRule{
		{Users: []string{"alice", "carol"}},
	})
	svc := newTestSandboxTemplateService(t, tmpl)

	// alice can see it
	items, appErr := svc.List(context.Background(), tenantAuth("anyTeam", "alice"), false)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 {
		t.Errorf("alice: expected 1 item, got %d", len(items))
	}

	// bob cannot see it
	items, appErr = svc.List(context.Background(), tenantAuth("anyTeam", "bob"), false)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 0 {
		t.Errorf("bob: expected 0 items, got %d", len(items))
	}
}

func TestVisibility_TeamAndUsersAND(t *testing.T) {
	// Rule requires BOTH team=teamA AND user=alice
	tmpl := makeTemplateWithVisibility("and-tmpl", []agentsv1alpha1.TemplateVisibilityRule{
		{Team: "teamA", Users: []string{"alice"}},
	})
	svc := newTestSandboxTemplateService(t, tmpl)

	// alice in teamA → visible
	items, _ := svc.List(context.Background(), tenantAuth("teamA", "alice"), false)
	if len(items) != 1 {
		t.Errorf("teamA/alice: expected 1, got %d", len(items))
	}

	// alice in teamB → NOT visible (wrong team)
	items, _ = svc.List(context.Background(), tenantAuth("teamB", "alice"), false)
	if len(items) != 0 {
		t.Errorf("teamB/alice: expected 0, got %d", len(items))
	}

	// bob in teamA → NOT visible (wrong user)
	items, _ = svc.List(context.Background(), tenantAuth("teamA", "bob"), false)
	if len(items) != 0 {
		t.Errorf("teamA/bob: expected 0, got %d", len(items))
	}
}

func TestVisibility_MultipleRulesOR(t *testing.T) {
	// Rule1: team=teamA, Rule2: users=[carol]
	tmpl := makeTemplateWithVisibility("or-tmpl", []agentsv1alpha1.TemplateVisibilityRule{
		{Team: "teamA"},
		{Users: []string{"carol"}},
	})
	svc := newTestSandboxTemplateService(t, tmpl)

	// teamA member → visible via rule1
	items, _ := svc.List(context.Background(), tenantAuth("teamA", "anyone"), false)
	if len(items) != 1 {
		t.Errorf("teamA: expected 1, got %d", len(items))
	}

	// carol in any team → visible via rule2
	items, _ = svc.List(context.Background(), tenantAuth("teamB", "carol"), false)
	if len(items) != 1 {
		t.Errorf("carol/teamB: expected 1, got %d", len(items))
	}

	// bob in teamB → NOT visible
	items, _ = svc.List(context.Background(), tenantAuth("teamB", "bob"), false)
	if len(items) != 0 {
		t.Errorf("teamB/bob: expected 0, got %d", len(items))
	}
}

func TestVisibility_AdminBypassesVisibility(t *testing.T) {
	// Restricted template: only teamA/alice
	tmpl := makeTemplateWithVisibility("restricted", []agentsv1alpha1.TemplateVisibilityRule{
		{Team: "teamA", Users: []string{"alice"}},
	})
	svc := newTestSandboxTemplateService(t, tmpl)

	// Admin with isAdmin=true sees everything regardless of their team/user
	items, appErr := svc.List(context.Background(), tenantAuth("teamB", "outsider"), true)
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if len(items) != 1 {
		t.Errorf("admin bypass: expected 1 item, got %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Semver validation tests
// ---------------------------------------------------------------------------

func TestValidateSemver_Valid(t *testing.T) {
	validCases := []string{"0.0.1", "1.2.3", "10.20.30", "0.0.0"}
	for _, v := range validCases {
		if err := validateSemver(v); err != nil {
			t.Errorf("validateSemver(%q) unexpected error: %v", v, err)
		}
	}
}

func TestValidateSemver_Invalid(t *testing.T) {
	invalidCases := []string{
		"v1.2.3",   // has "v" prefix
		"1.2",      // only two parts
		"1.2.3.4",  // four parts
		"1.a.3",    // non-numeric
		"",         // empty
		"1..3",     // empty component
		"1.2.3-rc", // pre-release suffix
	}
	for _, v := range invalidCases {
		if err := validateSemver(v); err == nil {
			t.Errorf("validateSemver(%q) expected error, got nil", v)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "0.9.9", 1},
		{"0.9.9", "1.0.0", -1},
		{"1.2.3", "1.2.3", 0},
		{"0.0.2", "0.0.1", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.10.0", "1.9.0", 1},
	}
	for _, c := range cases {
		got, err := compareSemver(c.a, c.b)
		if err != nil {
			t.Errorf("compareSemver(%q, %q): unexpected error: %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdate_VersionInvalidFormat_Returns400(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-a", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	_, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tmpl-a",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "v1.1.0", // invalid: has "v" prefix
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.36",
			},
		},
	}))
	if appErr == nil {
		t.Fatal("expected error for invalid version format, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestUpdate_VersionLowerThanCurrent_Returns400(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-a", "2.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	_, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tmpl-a",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.9.9", // lower than current 2.0.0
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.36",
			},
		},
	}))
	if appErr == nil {
		t.Fatal("expected error for version downgrade, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestUpdate_VersionEqualToCurrent_Returns400(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-a", "1.2.3")
	svc := newTestSandboxTemplateService(t, tmpl)

	_, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tmpl-a",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.2.3", // same version
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.36",
			},
		},
	}))
	if appErr == nil {
		t.Fatal("expected error for same version, got nil")
	}
	if appErr.Code != domain.ErrCodeBadRequest {
		t.Fatalf("expected ErrCodeBadRequest, got %d", appErr.Code)
	}
}

func TestUpdate_VersionHigherThanCurrent_Success(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-a", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	result, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tmpl-a",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     testTemplateVersion,
			Description: "Updated",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.37",
			},
		},
	}))
	if appErr != nil {
		t.Fatalf("unexpected error: %v", appErr)
	}
	if result.Version != testTemplateVersion {
		t.Fatalf("expected version %s, got %s", testTemplateVersion, result.Version)
	}
}

func TestUpdate_EmptyVersion_NoValidation(t *testing.T) {
	// Empty version should not trigger semver checks
	tmpl := makeSandboxTemplate("tmpl-a", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	result, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tmpl-a",
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     "", // no version provided
			Description: "Updated",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.37",
			},
		},
	}))
	if appErr != nil {
		t.Fatalf("unexpected error for empty version: %v", appErr)
	}
	// Version becomes empty (spec is replaced)
	if result.Version != "" {
		t.Fatalf("expected empty version, got %s", result.Version)
	}
}

// ---------------------------------------------------------------------------
// CreateOrUpdate tests
// ---------------------------------------------------------------------------

func TestSandboxTemplateService_CreateOrUpdate_Create(t *testing.T) {
	svc := newTestSandboxTemplateService(t) // empty store

	tmpl := makeSandboxTemplate("tmpl-upsert", "1.0.0")
	appErr := svc.CreateOrUpdate(context.Background(), tmpl)
	if appErr != nil {
		t.Fatalf("CreateOrUpdate (create path) error: %v", appErr)
	}

	got, getErr := svc.Get(context.Background(), "tmpl-upsert")
	if getErr != nil {
		t.Fatalf("Get() after CreateOrUpdate error: %v", getErr)
	}
	if got.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", got.Version, "1.0.0")
	}
}

func TestSandboxTemplateService_CreateOrUpdate_Update(t *testing.T) {
	existing := makeSandboxTemplate("tmpl-upsert", "1.0.0")
	svc := newTestSandboxTemplateService(t, existing)

	// Now upsert with a new version and description.
	updated := makeSandboxTemplate("tmpl-upsert", "2.0.0")
	updated.Spec.Description = "updated description"

	appErr := svc.CreateOrUpdate(context.Background(), updated)
	if appErr != nil {
		t.Fatalf("CreateOrUpdate (update path) error: %v", appErr)
	}

	got, getErr := svc.Get(context.Background(), "tmpl-upsert")
	if getErr != nil {
		t.Fatalf("Get() after CreateOrUpdate error: %v", getErr)
	}
	if got.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", got.Version, "2.0.0")
	}
	if got.Description != "updated description" {
		t.Errorf("Description = %q, want %q", got.Description, "updated description")
	}
}

func TestSandboxTemplateService_CreateOrUpdate_Idempotent(t *testing.T) {
	svc := newTestSandboxTemplateService(t)

	tmpl := makeSandboxTemplate("tmpl-idempotent", "1.0.0")

	// Create twice — should not error on the second call.
	if appErr := svc.CreateOrUpdate(context.Background(), tmpl); appErr != nil {
		t.Fatalf("first CreateOrUpdate error: %v", appErr)
	}
	if appErr := svc.CreateOrUpdate(context.Background(), tmpl); appErr != nil {
		t.Fatalf("second CreateOrUpdate (idempotent) error: %v", appErr)
	}
}

// ---------------------------------------------------------------------------
// Optimistic-lock Update tests
// ---------------------------------------------------------------------------

// fakeClientResourceVersion returns the resourceVersion assigned by the fake client
// by extracting it from the crdYaml returned by Get.
func extractResourceVersion(t *testing.T, crdYaml string) string {
	t.Helper()
	var raw agentsv1alpha1.SandboxTemplate
	if err := yaml.Unmarshal([]byte(crdYaml), &raw); err != nil {
		t.Fatalf("unmarshal crdYaml: %v", err)
	}
	return raw.ResourceVersion
}

func TestUpdate_OptimisticLock_HappyPath(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-lock", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	// GET to obtain crdYaml which contains the current resourceVersion.
	got, appErr := svc.Get(context.Background(), "tmpl-lock")
	if appErr != nil {
		t.Fatalf("get: %v", appErr)
	}

	rv := extractResourceVersion(t, got.CrdYaml)

	result, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "tmpl-lock",
			ResourceVersion: rv,
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     testTemplateVersion,
			Description: "updated via optimistic lock",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.37",
			},
		},
	}))
	if appErr != nil {
		t.Fatalf("Update with resourceVersion: %v", appErr)
	}
	if result.Version != testTemplateVersion {
		t.Fatalf("expected version %s, got %s", testTemplateVersion, result.Version)
	}
	if result.Description != "updated via optimistic lock" {
		t.Fatalf("expected updated description, got %s", result.Description)
	}
	if !strings.Contains(result.CrdYaml, "busybox:1.37") {
		t.Fatalf("crdYaml should contain busybox:1.37")
	}
}

func TestUpdate_OptimisticLock_AnnotationsPreserved(t *testing.T) {
	// Verify that when we PUT back the full crdYaml (including annotations),
	// annotations are not lost — this is the root-cause fix.
	tmpl := makeSandboxTemplate("tmpl-ann", "1.0.0")
	tmpl.Annotations = map[string]string{"agentbox.navix.sh/docs": "original docs"}
	svc := newTestSandboxTemplateService(t, tmpl)

	got, appErr := svc.Get(context.Background(), "tmpl-ann")
	if appErr != nil {
		t.Fatalf("get: %v", appErr)
	}
	// crdYaml must contain the annotation.
	if !strings.Contains(got.CrdYaml, "original docs") {
		t.Fatalf("crdYaml should contain the docs annotation, got: %s", got.CrdYaml)
	}

	rv := extractResourceVersion(t, got.CrdYaml)

	// Update: pass back the same annotation via the parsed object.
	_, appErr = svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "tmpl-ann",
			ResourceVersion: rv,
			Annotations:     map[string]string{"agentbox.navix.sh/docs": "original docs"},
		},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.0.1",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.36",
			},
		},
	}))
	if appErr != nil {
		t.Fatalf("Update: %v", appErr)
	}

	// Re-GET and verify annotation is still there.
	after, appErr := svc.Get(context.Background(), "tmpl-ann")
	if appErr != nil {
		t.Fatalf("get after update: %v", appErr)
	}
	if !strings.Contains(after.CrdYaml, "original docs") {
		t.Fatalf("annotation lost after update; crdYaml: %s", after.CrdYaml)
	}
}

func TestUpdate_OptimisticLock_StaleRV_ReturnsConflict(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-stale", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	// First update bumps the resourceVersion.
	got, _ := svc.Get(context.Background(), "tmpl-stale")
	rv := extractResourceVersion(t, got.CrdYaml)

	_, _ = svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl-stale", ResourceVersion: rv},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:                 "1.0.1",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{IdleImage: "busybox:1.37"},
		},
	}))

	// Second update with the same (now stale) resourceVersion should yield Conflict.
	_, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl-stale", ResourceVersion: rv},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:                 "1.0.2",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{IdleImage: "busybox:1.38"},
		},
	}))
	if appErr == nil {
		t.Fatal("expected conflict error for stale resourceVersion, got nil")
	}
	if appErr.Code != domain.ErrCodeConflict {
		t.Fatalf("expected ErrCodeConflict, got %d", appErr.Code)
	}
}

func TestUpdate_NoRV_UsesRetryPath(t *testing.T) {
	// Empty resourceVersion → falls through to RetryOnConflict/MergePatch path.
	tmpl := makeSandboxTemplate("tmpl-norv", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	result, appErr := svc.Update(context.Background(), ptr.To(agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl-norv"},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "1.0.1",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "busybox:1.37",
			},
		},
	}))
	if appErr != nil {
		t.Fatalf("Update (no-RV path): %v", appErr)
	}
	if result.Version != "1.0.1" {
		t.Fatalf("expected 1.0.1, got %s", result.Version)
	}
}

// ---------------------------------------------------------------------------
// CrdYaml content tests
// ---------------------------------------------------------------------------

func TestGet_CrdYaml_ContainsExpectedFields(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-yaml", "2.3.4")
	tmpl.Annotations = map[string]string{"agentbox.navix.sh/docs": "hello docs"}
	svc := newTestSandboxTemplateService(t, tmpl)

	got, appErr := svc.Get(context.Background(), "tmpl-yaml")
	if appErr != nil {
		t.Fatalf("get: %v", appErr)
	}

	if got.CrdYaml == "" {
		t.Fatal("crdYaml should not be empty")
	}
	for _, want := range []string{"tmpl-yaml", "2.3.4", "busybox:1.36", "hello docs"} {
		if !strings.Contains(got.CrdYaml, want) {
			t.Errorf("crdYaml missing %q; got:\n%s", want, got.CrdYaml)
		}
	}
	// managedFields must be stripped
	if strings.Contains(got.CrdYaml, "managedFields") {
		t.Error("crdYaml should not contain managedFields")
	}
}

func TestGet_CrdYaml_ContainsResourceVersion(t *testing.T) {
	tmpl := makeSandboxTemplate("tmpl-rv", "1.0.0")
	svc := newTestSandboxTemplateService(t, tmpl)

	got, appErr := svc.Get(context.Background(), "tmpl-rv")
	if appErr != nil {
		t.Fatalf("get: %v", appErr)
	}

	rv := extractResourceVersion(t, got.CrdYaml)
	if rv == "" {
		t.Fatal("crdYaml should include a non-empty resourceVersion")
	}
}
