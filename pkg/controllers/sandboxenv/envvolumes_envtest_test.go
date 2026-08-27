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

package sandboxenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// These tests run against a real API server (envtest) rather than a fake
// client, because what they verify is precisely what a fake client cannot show:
// whether the generated CRD's structural schema actually applies
// +kubebuilder:default=true to overrides.volumes[].readOnly, and whether its
// validation markers reject what we think they reject.
//
// The distinction matters for a security property. IsReadOnly() treats nil as
// read-only so the Go path is safe either way, but if the CRD default did not
// fire, an Env written by kubectl would store a nil that any future reader
// dereferencing the pointer directly would misread as read-write.

var (
	volEnv       *envtest.Environment
	volCfg       *rest.Config
	volK8sClient client.Client
)

func TestMain(m *testing.M) {
	if err := agentsv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Fprintf(os.Stderr, "add to scheme: %v\n", err)
		os.Exit(1)
	}
	volEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		volEnv.BinaryAssetsDirectory = dir
	}

	var err error
	volCfg, err = volEnv.Start()
	if err != nil {
		// No envtest assets available (e.g. a bare `go test` without
		// `make test`). Skip rather than fail: the unit tests in this package
		// still run, and CI always has the assets.
		fmt.Fprintf(os.Stderr, "envtest unavailable, skipping envtest-backed cases: %v\n", err)
		os.Exit(m.Run())
	}
	volK8sClient, err = client.New(volCfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = volEnv.Stop()
	os.Exit(code)
}

func firstEnvTestBinaryDir() string {
	base := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(base, e.Name())
		}
	}
	return ""
}

func requireEnvtest(t *testing.T) {
	t.Helper()
	if volK8sClient == nil {
		t.Skip("envtest assets unavailable")
	}
}

// newVolEnvObj builds a SandboxEnv carrying the given volume mounts.
func newVolEnvObj(name string, vols []agentsv1alpha1.EnvVolumeMount) *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Mode:        agentsv1alpha1.SandboxEnvModeWarmPool,
			TemplateRef: agentsv1alpha1.SandboxEnvTemplateRef{Name: "tmpl"},
			Overrides:   &agentsv1alpha1.EnvOverridesSpec{Volumes: vols},
		},
	}
}

func createAndRead(t *testing.T, obj *agentsv1alpha1.SandboxEnv) *agentsv1alpha1.SandboxEnv {
	t.Helper()
	ctx := context.Background()
	if err := volK8sClient.Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_ = volK8sClient.Delete(context.Background(), obj)
	})
	out := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := volK8sClient.Get(ctx, key, out); err == nil {
			return out
		} else if time.Now().After(deadline) {
			t.Fatalf("get: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestEnvtest_ReadOnlyDefaultsTrueOnTheAPIServer is the claim under test: an Env
// written without readOnly must come back with it set to true.
func TestEnvtest_ReadOnlyDefaultsTrueOnTheAPIServer(t *testing.T) {
	requireEnvtest(t)
	got := createAndRead(t, newVolEnvObj("vol-default", []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds"}, // readOnly omitted
	}))

	vols := got.Spec.Overrides.Volumes
	if len(vols) != 1 {
		t.Fatalf("want 1 volume, got %d", len(vols))
	}
	if vols[0].ReadOnly == nil {
		t.Fatal("the CRD default did not fire: readOnly is nil after a round trip " +
			"through the API server")
	}
	if !*vols[0].ReadOnly {
		t.Errorf("readOnly defaulted to %v, want true", *vols[0].ReadOnly)
	}
	if !vols[0].IsReadOnly() {
		t.Error("IsReadOnly() disagrees with the stored value")
	}
}

// An explicit false must survive; the default must not overwrite intent.
func TestEnvtest_ExplicitWritableSurvives(t *testing.T) {
	requireEnvtest(t)
	got := createAndRead(t, newVolEnvObj("vol-writable", []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "scratch", MountPath: "/volume/scratch", ReadOnly: ptr.To(false)},
	}))
	v := got.Spec.Overrides.Volumes[0]
	if v.ReadOnly == nil || *v.ReadOnly {
		t.Errorf("explicit readOnly=false must survive, got %v", v.ReadOnly)
	}
	if v.IsReadOnly() {
		t.Error("IsReadOnly() must report writable")
	}
}

// TestEnvtest_RequiredFieldsEnforced: the API server, not just our Go
// validator, refuses an entry missing claimName or mountPath.
func TestEnvtest_RequiredFieldsEnforced(t *testing.T) {
	requireEnvtest(t)
	cases := map[string]agentsv1alpha1.EnvVolumeMount{
		"missing claimName": {MountPath: "/volume/x"},
		"missing mountPath": {ClaimName: "ds"},
	}
	for name, vol := range cases {
		t.Run(name, func(t *testing.T) {
			obj := newVolEnvObj("vol-req-"+strings.ReplaceAll(name, " ", "-"),
				[]agentsv1alpha1.EnvVolumeMount{vol})
			err := volK8sClient.Create(context.Background(), obj)
			if err == nil {
				_ = volK8sClient.Delete(context.Background(), obj)
				t.Fatal("the API server accepted an entry missing a required field")
			}
		})
	}
}

// TestEnvtest_MaxItemsEnforced: the schema's MaxItems=8 is real, not just a
// comment. It backs up the Go validator for hand-written CRs.
func TestEnvtest_MaxItemsEnforced(t *testing.T) {
	requireEnvtest(t)
	vols := make([]agentsv1alpha1.EnvVolumeMount, 0, 9)
	for i := range 9 {
		vols = append(vols, agentsv1alpha1.EnvVolumeMount{
			ClaimName: "ds",
			MountPath: fmt.Sprintf("/volume/v%d", i),
		})
	}
	obj := newVolEnvObj("vol-maxitems", vols)
	err := volK8sClient.Create(context.Background(), obj)
	if err == nil {
		_ = volK8sClient.Delete(context.Background(), obj)
		t.Fatal("the API server accepted 9 volume mounts; MaxItems=8 is not enforced")
	}
	if !strings.Contains(err.Error(), "must have at most 8 items") {
		t.Errorf("unexpected rejection reason: %v", err)
	}
}

// TestEnvtest_SubPathRoundTrips guards against the field being pruned by the
// structural schema.
func TestEnvtest_SubPathRoundTrips(t *testing.T) {
	requireEnvtest(t)
	got := createAndRead(t, newVolEnvObj("vol-subpath", []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "models", MountPath: "/volume/models", SubPath: "Qwen/Qwen2.5-7B"},
	}))
	if got.Spec.Overrides.Volumes[0].SubPath != "Qwen/Qwen2.5-7B" {
		t.Errorf("subPath did not survive: %q", got.Spec.Overrides.Volumes[0].SubPath)
	}
}

// TestEnvtest_EmptyListIsDistinctFromAbsent: clearing the mounts has to be
// expressible through the API server too, not only in our Go types.
func TestEnvtest_EmptyListIsDistinctFromAbsent(t *testing.T) {
	requireEnvtest(t)
	obj := createAndRead(t, newVolEnvObj("vol-clear", []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds"},
	}))

	obj.Spec.Overrides.Volumes = []agentsv1alpha1.EnvVolumeMount{}
	if err := volK8sClient.Update(context.Background(), obj); err != nil {
		t.Fatalf("update: %v", err)
	}
	out := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}
	if err := volK8sClient.Get(context.Background(), key, out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(out.Spec.Overrides.Volumes) != 0 {
		t.Errorf("clearing the list did not take effect: %+v", out.Spec.Overrides.Volumes)
	}
}
