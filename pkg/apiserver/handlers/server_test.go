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

package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

const testTeamA = "teamA"

// ptrStr is a test helper.
func ptrStr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// genSpecToK8sSpec tests
// ---------------------------------------------------------------------------

func TestGenSpecToK8sSpec_BasicFields(t *testing.T) {
	spec := gen.SandboxTemplateSpec{
		Version:     ptrStr("v1.2.3"),
		Description: ptrStr("desc"),
		IdleImage:   ptrStr("idle:latest"),
	}
	result, err := genSpecToK8sSpec(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Version != "v1.2.3" {
		t.Errorf("Version: want v1.2.3, got %s", result.Version)
	}
	if result.Description != "desc" {
		t.Errorf("Description: want desc, got %s", result.Description)
	}
	if result.IdleImage != "idle:latest" {
		t.Errorf("IdleImage: want idle:latest, got %s", result.IdleImage)
	}
}

func TestGenSpecToK8sSpec_Runtimes(t *testing.T) {
	proto := "TCP"
	port := int32(8080)
	runtimes := []gen.Runtime{
		{Name: "envd", Port: &port, Protocol: &proto},
	}
	spec := gen.SandboxTemplateSpec{
		IdleImage: ptrStr("idle:latest"),
		Runtimes:  &runtimes,
	}
	result, err := genSpecToK8sSpec(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Runtimes) != 1 {
		t.Fatalf("want 1 runtime, got %d", len(result.Runtimes))
	}
	r := result.Runtimes[0]
	if r.Name != "envd" {
		t.Errorf("runtime Name: want envd, got %s", r.Name)
	}
	if r.Port == nil || *r.Port != 8080 {
		t.Errorf("runtime Port: want 8080, got %v", r.Port)
	}
	if r.Protocol == nil || *r.Protocol != corev1.Protocol("TCP") {
		t.Errorf("runtime Protocol: want TCP, got %v", r.Protocol)
	}
}

func TestGenSpecToK8sSpec_Visibility(t *testing.T) {
	team := testTeamA
	users := []string{"alice", "bob"}
	rules := []gen.VisibilityRule{{Team: &team, Users: &users}}
	spec := gen.SandboxTemplateSpec{
		IdleImage:  ptrStr("idle:latest"),
		Visibility: &gen.VisibilityConfig{Rules: &rules},
	}
	result, err := genSpecToK8sSpec(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Visibility == nil {
		t.Fatal("Visibility is nil")
	}
	if len(result.Visibility.Rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(result.Visibility.Rules))
	}
	rule := result.Visibility.Rules[0]
	if rule.Team != testTeamA {
		t.Errorf("Team: want teamA, got %s", rule.Team)
	}
	if len(rule.Users) != 2 || rule.Users[0] != "alice" {
		t.Errorf("Users: want [alice bob], got %v", rule.Users)
	}
}

func TestGenSpecToK8sSpec_TemplateParsing(t *testing.T) {
	templateYAML := `
spec:
  containers:
  - name: sandbox
    image: myorg/sandbox:latest
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
`
	spec := gen.SandboxTemplateSpec{
		IdleImage: ptrStr("idle:latest"),
		Template:  &templateYAML,
	}
	result, err := genSpecToK8sSpec(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Template == nil {
		t.Fatal("Template is nil after parsing")
	}
	if len(result.Template.Spec.Containers) != 1 {
		t.Fatalf("want 1 container, got %d", len(result.Template.Spec.Containers))
	}
	c := result.Template.Spec.Containers[0]
	if c.Name != "sandbox" {
		t.Errorf("Container name: want sandbox, got %s", c.Name)
	}
	if c.Image != "myorg/sandbox:latest" {
		t.Errorf("Container image: want myorg/sandbox:latest, got %s", c.Image)
	}
}

func TestGenSpecToK8sSpec_InvalidTemplateYAML(t *testing.T) {
	bad := "this: is: not: valid: yaml: ::::"
	spec := gen.SandboxTemplateSpec{
		IdleImage: ptrStr("idle:latest"),
		Template:  &bad,
	}
	_, err := genSpecToK8sSpec(spec)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ---------------------------------------------------------------------------
// domainTemplateSpecToGen tests (round-trip)
// ---------------------------------------------------------------------------

func TestDomainTemplateSpecToGen_BasicFields(t *testing.T) {
	k8sSpec := agentsv1alpha1.SandboxTemplateSpec{
		Version:     "v2.0.0",
		Description: "hello",
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:v2",
		},
	}
	result := domainTemplateSpecToGen(k8sSpec)
	if result.Version == nil || *result.Version != "v2.0.0" {
		t.Errorf("Version: want v2.0.0, got %v", result.Version)
	}
	if result.Description == nil || *result.Description != "hello" {
		t.Errorf("Description: want hello, got %v", result.Description)
	}
	if result.IdleImage == nil || *result.IdleImage != "idle:v2" {
		t.Errorf("IdleImage: want idle:v2, got %v", result.IdleImage)
	}
}

func TestDomainTemplateSpecToGen_RuntimesRoundTrip(t *testing.T) {
	proto := corev1.ProtocolTCP
	port := int32(49983)
	k8sSpec := agentsv1alpha1.SandboxTemplateSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:latest",
			Runtimes: []agentsv1alpha1.SandboxRuntimeSpec{
				{Name: "envd", Port: &port, Protocol: &proto},
			},
		},
	}
	result := domainTemplateSpecToGen(k8sSpec)
	if result.Runtimes == nil || len(*result.Runtimes) != 1 {
		t.Fatal("want 1 runtime in HTTP DTO")
	}
	r := (*result.Runtimes)[0]
	if r.Name != "envd" {
		t.Errorf("Name: want envd, got %s", r.Name)
	}
	if r.Port == nil || *r.Port != 49983 {
		t.Errorf("Port: want 49983, got %v", r.Port)
	}
	if r.Protocol == nil || *r.Protocol != "TCP" {
		t.Errorf("Protocol: want TCP, got %v", r.Protocol)
	}
}

func TestDomainTemplateSpecToGen_VisibilityRoundTrip(t *testing.T) {
	k8sSpec := agentsv1alpha1.SandboxTemplateSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:latest",
		},
		Visibility: &agentsv1alpha1.TemplateVisibility{
			Rules: []agentsv1alpha1.TemplateVisibilityRule{
				{Team: testTeamA, Users: []string{"alice"}},
			},
		},
	}
	result := domainTemplateSpecToGen(k8sSpec)
	if result.Visibility == nil || result.Visibility.Rules == nil {
		t.Fatal("Visibility/Rules is nil in HTTP DTO")
	}
	rules := *result.Visibility.Rules
	if len(rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(rules))
	}
	if rules[0].Team == nil || *rules[0].Team != testTeamA {
		t.Errorf("Team: want teamA, got %v", rules[0].Team)
	}
	if rules[0].Users == nil || (*rules[0].Users)[0] != "alice" {
		t.Errorf("Users: want [alice], got %v", rules[0].Users)
	}
}

func TestDomainTemplateSpecToGen_TemplateYAMLRoundTrip(t *testing.T) {
	k8sSpec := agentsv1alpha1.SandboxTemplateSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:latest",
			Template: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sandbox", Image: "myorg/sandbox:v1"},
					},
				},
			},
		},
	}
	result := domainTemplateSpecToGen(k8sSpec)
	if result.Template == nil {
		t.Fatal("Template string is nil in HTTP DTO")
	}
	// Re-parse and verify
	var pts corev1.PodTemplateSpec
	if err := yaml.Unmarshal([]byte(*result.Template), &pts); err != nil {
		t.Fatalf("failed to parse serialised template YAML: %v", err)
	}
	if len(pts.Spec.Containers) != 1 {
		t.Fatalf("want 1 container in round-tripped YAML, got %d", len(pts.Spec.Containers))
	}
	if pts.Spec.Containers[0].Image != "myorg/sandbox:v1" {
		t.Errorf("Image: want myorg/sandbox:v1, got %s", pts.Spec.Containers[0].Image)
	}
}

// ---------------------------------------------------------------------------
// Full spec round-trip: genSpecToK8sSpec → domainTemplateSpecToGen
// ---------------------------------------------------------------------------

func TestFullSpecRoundTrip(t *testing.T) {
	templateYAML := `spec:
  containers:
  - image: myorg/sb:v2
    name: sandbox
    resources:
      requests:
        cpu: "1"
        memory: 2Gi
`
	proto := "UDP"
	port := int32(9000)
	team := "devs"
	users := []string{"carol"}
	rules := []gen.VisibilityRule{{Team: &team, Users: &users}}

	httpSpec := gen.SandboxTemplateSpec{
		Version:     ptrStr("v3"),
		Description: ptrStr("full test"),
		IdleImage:   ptrStr("idle:v3"),
		Template:    &templateYAML,
		Runtimes:    &[]gen.Runtime{{Name: "swerex", Port: &port, Protocol: &proto}},
		Visibility:  &gen.VisibilityConfig{Rules: &rules},
	}

	// Step 1: HTTP → K8s
	k8sSpec, err := genSpecToK8sSpec(httpSpec)
	if err != nil {
		t.Fatalf("genSpecToK8sSpec error: %v", err)
	}

	// Step 2: K8s → HTTP DTO
	backSpec := domainTemplateSpecToGen(k8sSpec)

	// Version / Description / IdleImage
	if backSpec.Version == nil || *backSpec.Version != "v3" {
		t.Errorf("Version round-trip failed: %v", backSpec.Version)
	}

	// Runtimes
	if backSpec.Runtimes == nil || len(*backSpec.Runtimes) != 1 {
		t.Fatal("Runtimes round-trip failed: nil or wrong count")
	}
	rt := (*backSpec.Runtimes)[0]
	if rt.Name != "swerex" {
		t.Errorf("runtime Name: want swerex, got %s", rt.Name)
	}
	if rt.Protocol == nil || *rt.Protocol != "UDP" {
		t.Errorf("runtime Protocol: want UDP, got %v", rt.Protocol)
	}

	// Visibility
	if backSpec.Visibility == nil || backSpec.Visibility.Rules == nil {
		t.Fatal("Visibility.Rules is nil after round-trip")
	}
	backRules := *backSpec.Visibility.Rules
	if len(backRules) != 1 || backRules[0].Team == nil || *backRules[0].Team != "devs" {
		t.Errorf("Visibility rule Team: want devs, got %v", backRules)
	}

	// Template
	if backSpec.Template == nil {
		t.Fatal("Template YAML is nil after round-trip")
	}
	var pts corev1.PodTemplateSpec
	if err := yaml.Unmarshal([]byte(*backSpec.Template), &pts); err != nil {
		t.Fatalf("re-parsing template YAML: %v", err)
	}
	if len(pts.Spec.Containers) == 0 || pts.Spec.Containers[0].Image != "myorg/sb:v2" {
		t.Errorf("Template container image not preserved: %v", pts.Spec.Containers)
	}
}

// ---------------------------------------------------------------------------
// sandboxToGen tests — endpoints conversion
// ---------------------------------------------------------------------------

func TestSandboxToGen_EndpointsWithLogDir(t *testing.T) {
	logDir := "/tmp/envd.log"
	sb := &domain.Sandbox{
		SandboxID: "sb-123",
		Namespace: "default",
		PoolName:  "test-pool",
		PodName:   "pod-abc",
		Status:    "Running",
		ClaimedAt: "2026-01-01T00:00:00Z",
		Endpoints: map[string]domain.SandboxEndpoint{
			"envd": {URL: "http://gw/sandboxes/sb-123/49983", LogDir: logDir},
		},
	}

	result := sandboxToGen(sb)

	if result.Endpoints == nil {
		t.Fatal("Endpoints is nil")
	}
	ep, ok := (*result.Endpoints)["envd"]
	if !ok {
		t.Fatal("envd endpoint not found")
	}
	if ep.Url != "http://gw/sandboxes/sb-123/49983" {
		t.Errorf("URL: want http://gw/sandboxes/sb-123/49983, got %s", ep.Url)
	}
	if ep.LogDir == nil || *ep.LogDir != "/tmp/envd.log" {
		t.Errorf("LogDir: want /tmp/envd.log, got %v", ep.LogDir)
	}
}

func TestSandboxToGen_EndpointsWithoutLogDir(t *testing.T) {
	sb := &domain.Sandbox{
		SandboxID: "sb-456",
		Namespace: "default",
		PoolName:  "test-pool",
		PodName:   "pod-def",
		Status:    "Running",
		ClaimedAt: "2026-01-01T00:00:00Z",
		Endpoints: map[string]domain.SandboxEndpoint{
			"swerex": {URL: "http://gw/sandboxes/sb-456/8080"},
		},
	}

	result := sandboxToGen(sb)

	if result.Endpoints == nil {
		t.Fatal("Endpoints is nil")
	}
	ep, ok := (*result.Endpoints)["swerex"]
	if !ok {
		t.Fatal("swerex endpoint not found")
	}
	if ep.Url != "http://gw/sandboxes/sb-456/8080" {
		t.Errorf("URL: want http://gw/sandboxes/sb-456/8080, got %s", ep.Url)
	}
	if ep.LogDir != nil {
		t.Errorf("LogDir: want nil for empty logDir, got %v", *ep.LogDir)
	}
}

func TestSandboxToGen_NoEndpoints(t *testing.T) {
	sb := &domain.Sandbox{
		SandboxID: "sb-789",
		Namespace: "default",
		PoolName:  "test-pool",
		PodName:   "pod-ghi",
		Status:    "Starting",
		ClaimedAt: "2026-01-01T00:00:00Z",
	}

	result := sandboxToGen(sb)
	if result.Endpoints != nil {
		t.Errorf("Endpoints should be nil when no endpoints configured, got %v", result.Endpoints)
	}
}

// ---------------------------------------------------------------------------
// ReadinessProbe mapping tests
// ---------------------------------------------------------------------------

func TestGenSpecToK8sSpec_ReadinessProbe(t *testing.T) {
	probePath := "/healthz"
	port := int32(8080)
	runtimes := []gen.Runtime{
		{
			Name: "envd",
			Port: &port,
			ReadinessProbe: &gen.RuntimeReadinessProbe{
				HttpGet: &struct {
					Path *string `json:"path,omitempty"`
					Port int32   `json:"port"`
				}{
					Port: 8080,
					Path: &probePath,
				},
			},
		},
	}
	spec := gen.SandboxTemplateSpec{
		IdleImage: ptrStr("idle:latest"),
		Runtimes:  &runtimes,
	}

	result, err := genSpecToK8sSpec(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Runtimes) != 1 {
		t.Fatalf("want 1 runtime, got %d", len(result.Runtimes))
	}
	r := result.Runtimes[0]
	if r.ReadinessProbe == nil {
		t.Fatal("expected ReadinessProbe to be set")
	}
	if r.ReadinessProbe.HTTPGet == nil {
		t.Fatal("expected HTTPGet probe handler")
	}
	if r.ReadinessProbe.HTTPGet.Port.IntVal != 8080 {
		t.Errorf("Port: want 8080, got %d", r.ReadinessProbe.HTTPGet.Port.IntVal)
	}
	if r.ReadinessProbe.HTTPGet.Path != "/healthz" {
		t.Errorf("Path: want /healthz, got %s", r.ReadinessProbe.HTTPGet.Path)
	}
}

func TestGenSpecToK8sSpec_ReadinessProbeDefaultPath(t *testing.T) {
	port := int32(9090)
	// Path is nil → should default to "/"
	runtimes := []gen.Runtime{
		{
			Name: "envd",
			Port: &port,
			ReadinessProbe: &gen.RuntimeReadinessProbe{
				HttpGet: &struct {
					Path *string `json:"path,omitempty"`
					Port int32   `json:"port"`
				}{
					Port: 9090,
					Path: nil,
				},
			},
		},
	}
	spec := gen.SandboxTemplateSpec{
		IdleImage: ptrStr("idle:latest"),
		Runtimes:  &runtimes,
	}

	result, err := genSpecToK8sSpec(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := result.Runtimes[0]
	if r.ReadinessProbe == nil || r.ReadinessProbe.HTTPGet == nil {
		t.Fatal("expected HTTPGet probe")
	}
	if r.ReadinessProbe.HTTPGet.Path != "/" {
		t.Errorf("Path: want /, got %s", r.ReadinessProbe.HTTPGet.Path)
	}
}

func TestDomainTemplateSpecToGen_ReadinessProbeRoundTrip(t *testing.T) {
	// Build a K8s spec with a readinessProbe and verify domainTemplateSpecToGen
	// converts it back to gen types correctly.
	corev1Port := int32(8080)
	spec := agentsv1alpha1.SandboxTemplateSpec{
		EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
			IdleImage: "idle:latest",
			Runtimes: []agentsv1alpha1.SandboxRuntimeSpec{
				{
					Name: "envd",
					Port: &corev1Port,
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt32(8080),
								Path: "/ready",
							},
						},
					},
				},
			},
		},
	}

	result := domainTemplateSpecToGen(spec)
	if result.Runtimes == nil || len(*result.Runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %v", result.Runtimes)
	}
	r := (*result.Runtimes)[0]
	if r.ReadinessProbe == nil {
		t.Fatal("expected ReadinessProbe to be populated in gen output")
	}
	if r.ReadinessProbe.HttpGet == nil {
		t.Fatal("expected HttpGet to be set")
	}
	if r.ReadinessProbe.HttpGet.Port != 8080 {
		t.Errorf("Port: want 8080, got %d", r.ReadinessProbe.HttpGet.Port)
	}
	if r.ReadinessProbe.HttpGet.Path == nil || *r.ReadinessProbe.HttpGet.Path != "/ready" {
		t.Errorf("Path: want /ready, got %v", r.ReadinessProbe.HttpGet.Path)
	}
}
