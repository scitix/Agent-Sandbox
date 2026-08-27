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

package sandboxrender

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	validation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// embWithContainers builds a minimal template with one sandbox container.
func embWithContainers() *agentsv1alpha1.EmbeddedSandboxTemplate {
	return &agentsv1alpha1.EmbeddedSandboxTemplate{
		IdleImage: "reg.a.example.com/agentbox/idle:v1",
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "sandbox",
					Image: "reg.a.example.com/agentbox/base:v2",
					VolumeMounts: []corev1.VolumeMount{
						{Name: "shared-bin", MountPath: "/mnt/agentbox", ReadOnly: true},
					},
				}},
				InitContainers: []corev1.Container{{
					Name:  "envd",
					Image: "reg.a.example.com/agentbox/envd:0.3.1",
				}},
				Volumes: []corev1.Volume{{
					Name:         "shared-bin",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}},
			},
		},
	}
}

// TestOptionsEmpty_CountsVolumes guards the silent-no-op failure mode: if
// Empty() does not account for Volumes, Apply returns early and an Env whose
// only override is a volume mount renders to nothing at all.
func TestOptionsEmpty_CountsVolumes(t *testing.T) {
	o := Options{Volumes: []agentsv1alpha1.EnvVolumeMount{{ClaimName: "c", MountPath: "/v"}}}
	if o.Empty() {
		t.Fatal("Options carrying only Volumes must not be Empty(); Apply would skip rendering entirely")
	}
}

func TestOptionsEmpty_CountsImageRegistry(t *testing.T) {
	o := Options{ImageRegistry: &RegistryRewrite{
		LocalClusterID: "eu-west",
		Store: buildFakeStore(map[string][]registryEntrySpec{
			"eu-west": {{host: "eu-docker.pkg.dev", typ: "gar"}},
		}),
	}}
	if o.Empty() {
		t.Fatal("Options carrying only ImageRegistry must not be Empty()")
	}
}

func TestOptionsEmpty_TrulyEmpty(t *testing.T) {
	if !(Options{}).Empty() {
		t.Fatal("zero Options must be Empty()")
	}
}

// TestApplyVolumes_GroupsBySource is the core of the read-only design: readOnly
// lives on the volume SOURCE, so claims in different modes are different
// volumes, each carrying its own readOnly.
//
// Note the modes here belong to DIFFERENT claims. One claim in two modes is
// refused by ValidateVolumeMounts, because kubelet cannot mount a single
// PersistentVolumeClaim twice in a Pod — it would hang in ContainerCreating.
// The grouping key stays (claimName, readOnly) as defence in depth for a
// hand-edited CR that bypassed validation; it is not a reachable shape.
func TestApplyVolumes_GroupsBySource(t *testing.T) {
	emb := embWithContainers()
	err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "dataset", MountPath: "/volume/dataset", ReadOnly: ptr.To(true)},
		{ClaimName: "scratch", MountPath: "/volume/scratch", ReadOnly: ptr.To(false)},
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var pvcVols []corev1.Volume
	for _, v := range emb.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			pvcVols = append(pvcVols, v)
		}
	}
	if len(pvcVols) != 2 {
		t.Fatalf("want 2 PVC volumes for 2 claims, got %d", len(pvcVols))
	}
	byClaim := map[string]bool{}
	for _, v := range pvcVols {
		byClaim[v.PersistentVolumeClaim.ClaimName] = v.PersistentVolumeClaim.ReadOnly
	}
	if !byClaim["dataset"] {
		t.Error("the read-only claim must render a read-only volume source")
	}
	if byClaim["scratch"] {
		t.Error("the read-write claim must not render a read-only volume source")
	}
}

// TestApplyVolumes_SameSourceSharesOneVolume: two mount paths off the same
// claim in the same mode is one volume, two mounts.
func TestApplyVolumes_SameSourceSharesOneVolume(t *testing.T) {
	emb := embWithContainers()
	if err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "models", MountPath: "/volume/a", SubPath: "a"},
		{ClaimName: "models", MountPath: "/volume/b", SubPath: "b"},
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	pvc := 0
	for _, v := range emb.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			pvc++
		}
	}
	if pvc != 1 {
		t.Fatalf("want 1 volume for one claim+mode, got %d", pvc)
	}
	mounts := injectedMounts(emb)
	if len(mounts) != 2 {
		t.Fatalf("want 2 volumeMounts, got %d", len(mounts))
	}
	if mounts[0].Name != mounts[1].Name {
		t.Error("both mounts must reference the same derived volume name")
	}
}

// TestApplyVolumes_ReadOnlyDefaultsToTrue: a nil ReadOnly must render read-only
// on the volume source. The CRD default does not fire for objects built in Go.
func TestApplyVolumes_ReadOnlyDefaultsToTrue(t *testing.T) {
	emb := embWithContainers()
	if err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds"}, // ReadOnly nil
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, v := range emb.Template.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		if !v.PersistentVolumeClaim.ReadOnly {
			t.Error("nil ReadOnly must render as read-only on the volume source")
		}
	}
	for _, m := range injectedMounts(emb) {
		if !m.ReadOnly {
			t.Error("nil ReadOnly must mirror as read-only on the mount")
		}
	}
}

// TestApplyVolumes_WritableNeverSetsMountReadOnly: mirroring must never claim
// read-only when the source is read-write.
func TestApplyVolumes_WritableNeverSetsMountReadOnly(t *testing.T) {
	emb := embWithContainers()
	if err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "scratch", MountPath: "/volume/scratch", ReadOnly: ptr.To(false)},
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, m := range injectedMounts(emb) {
		if m.ReadOnly {
			t.Error("read-write entry must not set mount ReadOnly")
		}
	}
}

// TestApplyVolumes_NeverSetsRecursiveReadOnly: the field exists in
// k8s.io/api but requires Kubernetes >= 1.30 + containerd >= 2.0; on older
// clusters it is silently dropped, which looks protective while doing nothing.
func TestApplyVolumes_NeverSetsRecursiveReadOnly(t *testing.T) {
	emb := embWithContainers()
	if err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds", ReadOnly: ptr.To(true)},
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, m := range emb.Template.Spec.Containers[0].VolumeMounts {
		if m.RecursiveReadOnly != nil {
			t.Errorf("mount %q must leave RecursiveReadOnly nil, got %v", m.Name, *m.RecursiveReadOnly)
		}
	}
}

// TestApplyVolumes_MountsOnlyFirstContainer: init containers and any sidecar
// must never see a user's dataset.
func TestApplyVolumes_MountsOnlyFirstContainer(t *testing.T) {
	emb := embWithContainers()
	emb.Template.Spec.Containers = append(emb.Template.Spec.Containers,
		corev1.Container{Name: "sidecar", Image: "reg.a.example.com/agentbox/egress:v1"})

	if err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds"},
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(emb.Template.Spec.Containers[1].VolumeMounts) != 0 {
		t.Error("sidecar must not receive the PVC mount")
	}
	if len(emb.Template.Spec.InitContainers[0].VolumeMounts) != 0 {
		t.Error("init container must not receive the PVC mount")
	}
}

func TestApplyVolumes_NoContainersIsAnError(t *testing.T) {
	emb := embWithContainers()
	emb.Template.Spec.Containers = nil
	err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds"},
	}})
	if err == nil {
		t.Fatal("want an error when the template declares no containers")
	}
}

// TestApplyVolumes_Deterministic is the invariant that keeps idle Pods from
// rebuilding forever: ComputeRevisionHash marshals the whole Pool spec, so a
// map-iteration-ordered volume list would flip the hash on every reconcile.
func TestApplyVolumes_Deterministic(t *testing.T) {
	vols := []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "zeta", MountPath: "/volume/z"},
		{ClaimName: "alpha", MountPath: "/volume/a", ReadOnly: ptr.To(false)},
		{ClaimName: "mid", MountPath: "/volume/m"},
		{ClaimName: "alpha", MountPath: "/volume/a2", ReadOnly: ptr.To(false)},
	}
	first := embWithContainers()
	if err := Apply(first, Options{Volumes: vols}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for range 20 {
		again := embWithContainers()
		if err := Apply(again, Options{Volumes: vols}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if !reflect.DeepEqual(first.Template.Spec.Volumes, again.Template.Spec.Volumes) {
			t.Fatal("volumes render is not deterministic across calls")
		}
		if !reflect.DeepEqual(
			first.Template.Spec.Containers[0].VolumeMounts,
			again.Template.Spec.Containers[0].VolumeMounts) {
			t.Fatal("volumeMounts render is not deterministic across calls")
		}
	}
}

// TestApplyVolumes_ReorderIsSemanticNoOp follows from canonical ordering: a
// pure reorder of the declared list must not change the rendered spec, so it
// does not flip the revision hash and does not roll the pool.
func TestApplyVolumes_ReorderIsSemanticNoOp(t *testing.T) {
	a := []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "one", MountPath: "/volume/1"},
		{ClaimName: "two", MountPath: "/volume/2", ReadOnly: ptr.To(false)},
		{ClaimName: "three", MountPath: "/volume/3"},
	}
	b := []agentsv1alpha1.EnvVolumeMount{a[2], a[0], a[1]}

	x, y := embWithContainers(), embWithContainers()
	if err := Apply(x, Options{Volumes: a}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := Apply(y, Options{Volumes: b}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !reflect.DeepEqual(x.Template.Spec.Volumes, y.Template.Spec.Volumes) {
		t.Error("reordering the declared list changed spec.volumes")
	}
	if !reflect.DeepEqual(
		x.Template.Spec.Containers[0].VolumeMounts,
		y.Template.Spec.Containers[0].VolumeMounts) {
		t.Error("reordering the declared list changed volumeMounts")
	}
}

func TestApplyVolumes_PreservesTemplateVolumes(t *testing.T) {
	emb := embWithContainers()
	if err := Apply(emb, Options{Volumes: []agentsv1alpha1.EnvVolumeMount{
		{ClaimName: "ds", MountPath: "/volume/ds"},
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if emb.Template.Spec.Volumes[0].Name != "shared-bin" {
		t.Error("template volumes must be preserved and come first")
	}
	if emb.Template.Spec.Containers[0].VolumeMounts[0].MountPath != "/mnt/agentbox" {
		t.Error("template volumeMounts must be preserved and come first")
	}
}

func TestVolumeNameFor(t *testing.T) {
	t.Run("distinct per mode", func(t *testing.T) {
		if VolumeNameFor("c", true) == VolumeNameFor("c", false) {
			t.Error("ro and rw must differ")
		}
	})

	// Pin the exact scheme. The derived name feeds the revision hash, so
	// changing it rolls every pool that mounts a volume — that must be a
	// deliberate act with a failing test, not a silent side effect of a
	// refactor.
	t.Run("scheme is pinned", func(t *testing.T) {
		cases := map[string]string{
			"team-data-41|ro":  "abx-vol-team-data-41-ro-0ccf0504",
			"team-data-41|rw":  "abx-vol-team-data-41-rw-10377c0e",
			"shared-models|ro": "abx-vol-shared-models-ro-7fb13464",
			"shared-models|rw": "abx-vol-shared-models-rw-9c361ab3",
		}
		for key, want := range cases {
			claim, mode, _ := strings.Cut(key, "|")
			if got := VolumeNameFor(claim, mode == "ro"); got != want {
				t.Errorf("VolumeNameFor(%q, ro=%v) = %q, want %q", claim, mode == "ro", got, want)
			}
		}
	})

	t.Run("prefixed", func(t *testing.T) {
		if !strings.HasPrefix(VolumeNameFor("c", true), ReservedVolumeNamePrefix) {
			t.Error("must carry the reserved prefix")
		}
	})

	t.Run("sanitiser collisions are broken by the digest", func(t *testing.T) {
		// "a.b" and "a-b" fold to the same sanitised form.
		if VolumeNameFor("a.b", true) == VolumeNameFor("a-b", true) {
			t.Error("claims differing only in a folded character must not collide")
		}
	})

	t.Run("DNS-1123 label valid and bounded", func(t *testing.T) {
		cases := []string{
			"c",
			"team-data-41",
			"shared-models",
			"UPPER.Case_Claim",
			strings.Repeat("very-long-claim-name", 13), // 260 chars
			"....",
			"9-leading-digit",
		}
		for _, claim := range cases {
			for _, ro := range []bool{true, false} {
				name := VolumeNameFor(claim, ro)
				if len(name) > maxVolumeNameLen {
					t.Errorf("claim %q ro=%v: name %q is %d chars, limit %d",
						claim, ro, name, len(name), maxVolumeNameLen)
				}
				if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
					t.Errorf("claim %q ro=%v: name %q is not a valid DNS-1123 label: %v",
						claim, ro, name, errs)
				}
			}
		}
	})
}

func TestEnvVolumeMountIsReadOnly(t *testing.T) {
	if !(agentsv1alpha1.EnvVolumeMount{}).IsReadOnly() {
		t.Error("nil ReadOnly must mean read-only")
	}
	if !(agentsv1alpha1.EnvVolumeMount{ReadOnly: ptr.To(true)}).IsReadOnly() {
		t.Error("explicit true must mean read-only")
	}
	if (agentsv1alpha1.EnvVolumeMount{ReadOnly: ptr.To(false)}).IsReadOnly() {
		t.Error("explicit false must mean read-write")
	}
}

// injectedMounts returns the volumeMounts Apply added, i.e. those referencing a
// renderer-owned volume name.
func injectedMounts(emb *agentsv1alpha1.EmbeddedSandboxTemplate) []corev1.VolumeMount {
	var out []corev1.VolumeMount
	for _, m := range emb.Template.Spec.Containers[0].VolumeMounts {
		if strings.HasPrefix(m.Name, ReservedVolumeNamePrefix) {
			out = append(out, m)
		}
	}
	return out
}
