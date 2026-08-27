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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"

	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const volTestTemplate = "tmpl-vol"

func boundPVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: envTestNamespace},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
}

func pendingPVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: envTestNamespace},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
}

// volTemplate builds a SandboxTemplate. privileged and runtimeClass exercise
// the two rejection paths that need the resolved Template.
func volTemplate(privileged bool, runtimeClass string, annotations map[string]string) *agentsv1alpha1.SandboxTemplate {
	c := corev1.Container{
		Name:  "sandbox",
		Image: "base:v1",
		VolumeMounts: []corev1.VolumeMount{
			{Name: "shared-bin", MountPath: "/mnt/agentbox", ReadOnly: true},
		},
	}
	if privileged {
		c.SecurityContext = &corev1.SecurityContext{Privileged: ptr.To(true)}
	}
	spec := corev1.PodSpec{
		Containers: []corev1.Container{c},
		Volumes: []corev1.Volume{{
			Name:         "shared-bin",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		}},
	}
	if runtimeClass != "" {
		spec.RuntimeClassName = ptr.To(runtimeClass)
	}
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: volTestTemplate, Annotations: annotations},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version: "v1",
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template:  corev1.PodTemplateSpec{Spec: spec},
			},
		},
	}
}

// newVolEnvService builds a service with the given VolumeConfig plus arbitrary
// seed objects (templates, PVCs, envs).
func newVolEnvService(t *testing.T, cfg VolumeConfig, objs ...client.Object) *k8sSandboxEnvService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("client builder: %v", err)
	}
	for _, o := range objs {
		cb = cb.WithObjects(o)
	}
	c := cb.Build()
	return NewSandboxEnvService(c, nil, nil, nil, nil, c, cfg).(*k8sSandboxEnvService)
}

func volOverrides(vols ...agentsv1alpha1.EnvVolumeMount) *agentsv1alpha1.EnvOverridesSpec {
	return &agentsv1alpha1.EnvOverridesSpec{Volumes: vols}
}

// TestValidateEnvVolumes_FeatureGate: the gate has to reject the write, not
// merely hide the dashboard panel. The failure mode it guards is an agent with
// write access to a user's dataset.
func TestValidateEnvVolumes_FeatureGate(t *testing.T) {
	vols := volOverrides(agentsv1alpha1.EnvVolumeMount{ClaimName: "ds", MountPath: "/volume/ds"})

	t.Run("disabled rejects", func(t *testing.T) {
		svc := newVolEnvService(t, VolumeConfig{Enabled: false},
			volTemplate(false, "", nil), boundPVC("ds"))
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate, vols)
		if err == nil || !strings.Contains(err.Message, "not enabled") {
			t.Fatalf("want a rejection naming the gate, got %v", err)
		}
	})

	t.Run("disabled still allows an Env with no volumes", func(t *testing.T) {
		svc := newVolEnvService(t, VolumeConfig{Enabled: false}, volTemplate(false, "", nil))
		if err := svc.validateEnvVolumes(
			context.Background(), envTestNamespace, volTestTemplate, &agentsv1alpha1.EnvOverridesSpec{}); err != nil {
			t.Fatalf("want accepted, got %v", err)
		}
	})

	t.Run("enabled accepts", func(t *testing.T) {
		svc := newVolEnvService(t, VolumeConfig{Enabled: true},
			volTemplate(false, "", nil), boundPVC("ds"))
		if err := svc.validateEnvVolumes(
			context.Background(), envTestNamespace, volTestTemplate, vols); err != nil {
			t.Fatalf("want accepted, got %v", err)
		}
	})
}

func TestValidateEnvVolumes_ClaimPreflight(t *testing.T) {
	cfg := VolumeConfig{Enabled: true}
	vols := volOverrides(agentsv1alpha1.EnvVolumeMount{ClaimName: "ds", MountPath: "/volume/ds"})

	t.Run("missing claim", func(t *testing.T) {
		svc := newVolEnvService(t, cfg, volTemplate(false, "", nil))
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate, vols)
		if err == nil || !strings.Contains(err.Message, "does not exist") {
			t.Fatalf("want a rejection for the missing claim, got %v", err)
		}
	})

	t.Run("unbound claim", func(t *testing.T) {
		svc := newVolEnvService(t, cfg, volTemplate(false, "", nil), pendingPVC("ds"))
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate, vols)
		if err == nil || !strings.Contains(err.Message, "not Bound") {
			t.Fatalf("want a rejection for the unbound claim, got %v", err)
		}
	})

	t.Run("claim in another namespace is not reachable", func(t *testing.T) {
		other := boundPVC("ds")
		other.Namespace = "someone-else"
		svc := newVolEnvService(t, cfg, volTemplate(false, "", nil), other)
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate, vols)
		if err == nil || !strings.Contains(err.Message, "does not exist") {
			t.Fatalf("a claim in another namespace must not satisfy the preflight, got %v", err)
		}
	})

	t.Run("each distinct claim is checked once", func(t *testing.T) {
		svc := newVolEnvService(t, cfg, volTemplate(false, "", nil), boundPVC("a"))
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate,
			volOverrides(
				agentsv1alpha1.EnvVolumeMount{ClaimName: "a", MountPath: "/volume/a1"},
				agentsv1alpha1.EnvVolumeMount{ClaimName: "a", MountPath: "/volume/a2"},
				agentsv1alpha1.EnvVolumeMount{ClaimName: "missing", MountPath: "/volume/m"},
			))
		if err == nil || !strings.Contains(err.Message, `"missing"`) {
			t.Fatalf("want the missing claim named, got %v", err)
		}
	})
}

// TestValidateEnvVolumes_ReadOnlyEnforceability is the privileged-template rule,
// including the narrowing that keeps privileged Docker-in-Docker working.
func TestValidateEnvVolumes_ReadOnlyEnforceability(t *testing.T) {
	cfg := VolumeConfig{Enabled: true}
	roVol := volOverrides(agentsv1alpha1.EnvVolumeMount{ClaimName: "ds", MountPath: "/volume/ds"})
	rwVol := volOverrides(agentsv1alpha1.EnvVolumeMount{
		ClaimName: "ds", MountPath: "/volume/ds", ReadOnly: ptr.To(false)})

	t.Run("privileged template refuses a read-only mount", func(t *testing.T) {
		svc := newVolEnvService(t, cfg, volTemplate(true, "", nil), boundPVC("ds"))
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate, roVol)
		if err == nil || !strings.Contains(err.Message, "privileged") {
			t.Fatalf("want a rejection naming privileged, got %v", err)
		}
	})

	t.Run("privileged template accepts a writable mount", func(t *testing.T) {
		svc := newVolEnvService(t, cfg, volTemplate(true, "", nil), boundPVC("ds"))
		if err := svc.validateEnvVolumes(
			context.Background(), envTestNamespace, volTestTemplate, rwVol); err != nil {
			t.Fatalf("a writable mount never claimed the guarantee; want accepted, got %v", err)
		}
	})

	t.Run("admin annotation opts the template out", func(t *testing.T) {
		svc := newVolEnvService(t, cfg,
			volTemplate(true, "", map[string]string{
				agentsv1alpha1.AllowUnenforceableReadOnlyVolumesAnnotationKey: "true",
			}),
			boundPVC("ds"))
		if err := svc.validateEnvVolumes(
			context.Background(), envTestNamespace, volTestTemplate, roVol); err != nil {
			t.Fatalf("the annotation must permit it, got %v", err)
		}
	})
}

func TestValidateEnvVolumes_RuntimeClassAllowlist(t *testing.T) {
	vols := volOverrides(agentsv1alpha1.EnvVolumeMount{ClaimName: "ds", MountPath: "/volume/ds"})

	t.Run("default runtime is allowed", func(t *testing.T) {
		svc := newVolEnvService(t, VolumeConfig{Enabled: true},
			volTemplate(false, "", nil), boundPVC("ds"))
		if err := svc.validateEnvVolumes(
			context.Background(), envTestNamespace, volTestTemplate, vols); err != nil {
			t.Fatalf("want accepted, got %v", err)
		}
	})

	t.Run("unlisted runtime class is refused", func(t *testing.T) {
		svc := newVolEnvService(t, VolumeConfig{Enabled: true},
			volTemplate(false, "kata-fc-115", nil), boundPVC("ds"))
		err := svc.validateEnvVolumes(context.Background(), envTestNamespace, volTestTemplate, vols)
		if err == nil || !strings.Contains(err.Message, "kata-fc-115") {
			t.Fatalf("want the runtime class named in the rejection, got %v", err)
		}
	})

	t.Run("allowlisted runtime class is accepted", func(t *testing.T) {
		svc := newVolEnvService(t, VolumeConfig{
			Enabled:               true,
			AllowedRuntimeClasses: []string{"kata-fc-115"},
		}, volTemplate(false, "kata-fc-115", nil), boundPVC("ds"))
		if err := svc.validateEnvVolumes(
			context.Background(), envTestNamespace, volTestTemplate, vols); err != nil {
			t.Fatalf("want accepted once allowlisted, got %v", err)
		}
	})
}

func TestValidateEnvVolumes_MissingTemplate(t *testing.T) {
	svc := newVolEnvService(t, VolumeConfig{Enabled: true}, boundPVC("ds"))
	err := svc.validateEnvVolumes(context.Background(), envTestNamespace, "nope",
		volOverrides(agentsv1alpha1.EnvVolumeMount{ClaimName: "ds", MountPath: "/volume/ds"}))
	if err == nil || !strings.Contains(err.Message, "not found") {
		t.Fatalf("want a not-found error for the template, got %v", err)
	}
}

// TestUpdate_OverridesAreReplacedWholesale_NotMerged pins decision B. PATCH
// replaces the whole overrides block; the only existing client relies on
// omission-means-clear to remove a network policy, and this is also what lets a
// caller delete the last volume mount.
func TestUpdate_OverridesAreReplacedWholesale_NotMerged(t *testing.T) {
	env := newEnv(envTestName, "team-1", "user-1")
	env.Spec.TemplateRef.Name = volTestTemplate
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		Image: "custom:v1",
		NetworkPolicy: &agentsv1alpha1.SandboxNetworkPolicy{
			DisableEgress: true,
		},
		Volumes: []agentsv1alpha1.EnvVolumeMount{
			{ClaimName: "ds", MountPath: "/volume/ds", ReadOnly: ptr.To(true)},
		},
	}
	svc := newVolEnvService(t, VolumeConfig{Enabled: true},
		env, volTemplate(false, "", nil), boundPVC("ds"), boundPVC("other"))

	// PATCH carrying only volumes must wipe image and networkPolicy.
	if _, err := svc.Update(context.Background(), UpdateSandboxEnvInput{
		Name:      envTestName,
		Namespace: envTestNamespace,
		Overrides: volOverrides(agentsv1alpha1.EnvVolumeMount{
			ClaimName: "other", MountPath: "/volume/other"}),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	stored := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: envTestNamespace, Name: envTestName}
	if err := svc.client.Get(context.Background(), key, stored); err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Spec.Overrides.Image != "" {
		t.Errorf("image should have been replaced away, got %q", stored.Spec.Overrides.Image)
	}
	if stored.Spec.Overrides.NetworkPolicy != nil {
		t.Error("networkPolicy should have been replaced away")
	}
	if len(stored.Spec.Overrides.Volumes) != 1 ||
		stored.Spec.Overrides.Volumes[0].ClaimName != "other" {
		t.Errorf("volumes not replaced as sent: %+v", stored.Spec.Overrides.Volumes)
	}
}

// TestUpdate_EmptyVolumeListClearsMounts: removing the last mount has to be
// expressible, which is the property that made wholesale replace the right
// choice over a field-wise merge.
func TestUpdate_EmptyVolumeListClearsMounts(t *testing.T) {
	env := newEnv(envTestName, "team-1", "user-1")
	env.Spec.TemplateRef.Name = volTestTemplate
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		Volumes: []agentsv1alpha1.EnvVolumeMount{
			{ClaimName: "ds", MountPath: "/volume/ds"},
		},
	}
	svc := newVolEnvService(t, VolumeConfig{Enabled: true},
		env, volTemplate(false, "", nil), boundPVC("ds"))

	if _, err := svc.Update(context.Background(), UpdateSandboxEnvInput{
		Name:      envTestName,
		Namespace: envTestNamespace,
		Overrides: &agentsv1alpha1.EnvOverridesSpec{Volumes: []agentsv1alpha1.EnvVolumeMount{}},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	stored := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: envTestNamespace, Name: envTestName}
	if err := svc.client.Get(context.Background(), key, stored); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(stored.Spec.Overrides.Volumes) != 0 {
		t.Errorf("an empty list must clear the mounts, got %+v", stored.Spec.Overrides.Volumes)
	}
}

// TestUpdate_RejectsBadVolumesWithoutWriting: the preflight refuses before
// anything is persisted, mirroring preflightInjectedCredentials.
func TestUpdate_RejectsBadVolumesWithoutWriting(t *testing.T) {
	env := newEnv(envTestName, "team-1", "user-1")
	env.Spec.TemplateRef.Name = volTestTemplate
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{Image: "keep-me:v1"}
	svc := newVolEnvService(t, VolumeConfig{Enabled: true}, env, volTemplate(false, "", nil))

	_, err := svc.Update(context.Background(), UpdateSandboxEnvInput{
		Name:      envTestName,
		Namespace: envTestNamespace,
		Overrides: volOverrides(agentsv1alpha1.EnvVolumeMount{
			ClaimName: "nonexistent", MountPath: "/volume/x"}),
	})
	if err == nil {
		t.Fatal("want a rejection for the missing claim")
	}

	stored := &agentsv1alpha1.SandboxEnv{}
	key := types.NamespacedName{Namespace: envTestNamespace, Name: envTestName}
	if gerr := svc.client.Get(context.Background(), key, stored); gerr != nil {
		t.Fatalf("get: %v", gerr)
	}
	if stored.Spec.Overrides.Image != "keep-me:v1" {
		t.Errorf("a rejected update must not have written anything, got image %q",
			stored.Spec.Overrides.Image)
	}
}
