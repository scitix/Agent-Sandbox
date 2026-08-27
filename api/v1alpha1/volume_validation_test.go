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

package v1alpha1

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// sandboxPodSpec is the shape a real sandbox template has: one container that
// mounts the injected tooling read-only at /mnt/agentbox.
func sandboxPodSpec() *corev1.PodSpec {
	return &corev1.PodSpec{
		Containers: []corev1.Container{{
			Name: "sandbox",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "shared-bin", MountPath: "/mnt/agentbox", ReadOnly: true},
			},
		}},
		Volumes: []corev1.Volume{{
			Name:         "shared-bin",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		}},
	}
}

func TestValidateVolumeMounts_Accepts(t *testing.T) {
	cases := []struct {
		name string
		vols []EnvVolumeMount
	}{
		{"empty", nil},
		{"single read-only", []EnvVolumeMount{
			{ClaimName: "shared-models", MountPath: "/volume/models"},
		}},
		{"subPath", []EnvVolumeMount{
			{ClaimName: "shared-models", MountPath: "/volume/models", SubPath: "Qwen/Qwen2.5-7B-Instruct"},
		}},
		{"claim name with dots is a valid PVC name", []EnvVolumeMount{
			{ClaimName: "team.project.data", MountPath: "/volume/d"},
		}},
		{"same claim, same mode, several paths", []EnvVolumeMount{
			{ClaimName: "data", MountPath: "/volume/shared", SubPath: "shared", ReadOnly: ptr.To(true)},
			{ClaimName: "data", MountPath: "/volume/me", SubPath: "me", ReadOnly: ptr.To(true)},
		}},
		{"two different claims in different modes", []EnvVolumeMount{
			{ClaimName: "dataset", MountPath: "/volume/dataset", ReadOnly: ptr.To(true)},
			{ClaimName: "scratch", MountPath: "/volume/scratch", ReadOnly: ptr.To(false)},
		}},
		{"sibling of a template mount", []EnvVolumeMount{
			{ClaimName: "data", MountPath: "/mnt/data"},
		}},
		{"at the cap", []EnvVolumeMount{
			{ClaimName: "a", MountPath: "/volume/1"},
			{ClaimName: "a", MountPath: "/volume/2"},
			{ClaimName: "a", MountPath: "/volume/3"},
			{ClaimName: "a", MountPath: "/volume/4"},
			{ClaimName: "a", MountPath: "/volume/5"},
			{ClaimName: "a", MountPath: "/volume/6"},
			{ClaimName: "a", MountPath: "/volume/7"},
			{ClaimName: "a", MountPath: "/volume/8"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateVolumeMounts(tc.vols, sandboxPodSpec()); err != nil {
				t.Errorf("want accepted, got error: %v", err)
			}
		})
	}
}

func TestValidateVolumeMounts_Rejects(t *testing.T) {
	cases := []struct {
		name     string
		vols     []EnvVolumeMount
		wantSubs string
	}{
		{"missing claimName",
			[]EnvVolumeMount{{MountPath: "/volume/a"}}, "claimName is required"},
		{"invalid claimName",
			[]EnvVolumeMount{{ClaimName: "Bad_Name", MountPath: "/volume/a"}}, "not a valid PersistentVolumeClaim name"},
		{"missing mountPath",
			[]EnvVolumeMount{{ClaimName: "a"}}, "mountPath is required"},
		{"relative mountPath",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "volume/a"}}, "must be an absolute path"},
		{"unclean mountPath",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/volume//a/"}}, "not a clean path"},
		{"root mountPath",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/"}}, "may not be mounted directly"},
		{"/mnt itself",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/mnt"}}, "may not be mounted directly"},
		{"inside /proc",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/proc/self"}}, "reserved path"},
		{"inside /etc",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/etc/ssl"}}, "reserved path"},
		{"inside /var/lib/kubelet",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/var/lib/kubelet/pods"}}, "reserved path"},
		{"absolute subPath",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/volume/a", SubPath: "/abs"}}, "must be relative"},
		{"subPath backstep",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/volume/a", SubPath: "../../etc"}}, "may not contain"},
		{"unclean subPath",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/volume/a", SubPath: "x//y"}}, "not a clean path"},
		{"duplicate mountPath",
			[]EnvVolumeMount{
				{ClaimName: "a", MountPath: "/volume/a"},
				{ClaimName: "b", MountPath: "/volume/a"},
			}, "declared more than once"},
		{"nested declared paths",
			[]EnvVolumeMount{
				{ClaimName: "a", MountPath: "/volume/a"},
				{ClaimName: "b", MountPath: "/volume/a/inner"},
			}, "nests with another declared mount"},
		{"collides with the template's own mount",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/mnt/agentbox"}}, "already mounted by container"},
		{"inside the template's own mount",
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/mnt/agentbox/x"}}, "already mounted by"},
		{"too many entries",
			[]EnvVolumeMount{
				{ClaimName: "a", MountPath: "/volume/1"}, {ClaimName: "a", MountPath: "/volume/2"},
				{ClaimName: "a", MountPath: "/volume/3"}, {ClaimName: "a", MountPath: "/volume/4"},
				{ClaimName: "a", MountPath: "/volume/5"}, {ClaimName: "a", MountPath: "/volume/6"},
				{ClaimName: "a", MountPath: "/volume/7"}, {ClaimName: "a", MountPath: "/volume/8"},
				{ClaimName: "a", MountPath: "/volume/9"},
			}, "at most 8 volume mounts"},
		{"same claim in both modes",
			[]EnvVolumeMount{
				{ClaimName: "data", MountPath: "/volume/ro", ReadOnly: ptr.To(true)},
				{ClaimName: "data", MountPath: "/volume/rw", ReadOnly: ptr.To(false)},
			}, "declared both read-only and read-write"},
		{"same claim in both modes, one mode implicit",
			[]EnvVolumeMount{
				{ClaimName: "data", MountPath: "/volume/ro"},
				{ClaimName: "data", MountPath: "/volume/rw", ReadOnly: ptr.To(false)},
			}, "declared both read-only and read-write"},
		{"too many distinct sources",
			[]EnvVolumeMount{
				{ClaimName: "a", MountPath: "/volume/a"},
				{ClaimName: "b", MountPath: "/volume/b"},
				{ClaimName: "c", MountPath: "/volume/c"},
				{ClaimName: "d", MountPath: "/volume/d"},
				{ClaimName: "e", MountPath: "/volume/e"},
			}, "at most 4 distinct volume sources"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVolumeMounts(tc.vols, sandboxPodSpec())
			if err == nil {
				t.Fatalf("want rejection containing %q, got nil", tc.wantSubs)
			}
			if !strings.Contains(err.Error(), tc.wantSubs) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSubs)
			}
		})
	}
}

// TestValidateVolumeMounts_SourceCapCountsDistinctClaims: each claim is one
// corev1.Volume and therefore one mount operation at Pod creation, which is
// what the cap bounds. Several mount paths off one claim share its volume and
// so count once.
func TestValidateVolumeMounts_SourceCapCountsDistinctClaims(t *testing.T) {
	t.Run("five claims exceed the cap", func(t *testing.T) {
		vols := []EnvVolumeMount{
			{ClaimName: "a", MountPath: "/volume/a"},
			{ClaimName: "b", MountPath: "/volume/b"},
			{ClaimName: "c", MountPath: "/volume/c"},
			{ClaimName: "d", MountPath: "/volume/d"},
			{ClaimName: "e", MountPath: "/volume/e"},
		}
		err := ValidateVolumeMounts(vols, sandboxPodSpec())
		if err == nil || !strings.Contains(err.Error(), "at most 4 distinct volume sources") {
			t.Fatalf("want the source cap to reject 5 claims, got %v", err)
		}
	})

	t.Run("many paths off few claims stay under the cap", func(t *testing.T) {
		vols := []EnvVolumeMount{
			{ClaimName: "a", MountPath: "/volume/a1", SubPath: "1"},
			{ClaimName: "a", MountPath: "/volume/a2", SubPath: "2"},
			{ClaimName: "a", MountPath: "/volume/a3", SubPath: "3"},
			{ClaimName: "b", MountPath: "/volume/b1", SubPath: "1"},
			{ClaimName: "b", MountPath: "/volume/b2", SubPath: "2"},
		}
		if err := ValidateVolumeMounts(vols, sandboxPodSpec()); err != nil {
			t.Fatalf("two claims across five paths is within the cap, got %v", err)
		}
	})
}

// TestValidateVolumeMounts_SameClaimBothModesRejected pins the constraint a real
// cluster taught us: kubelet cannot mount one PersistentVolumeClaim twice in a
// Pod. Two modes need two corev1.Volume entries for the same claim, the volume
// manager keys by the underlying volume, and the Pod hangs in
// ContainerCreating until its mount deadline with no useful event. Serving both
// modes off one volume would instead require making the source read-write and
// leaving the read-only mount enforced only by the mount flag — the weaker
// spelling this feature exists to avoid.
func TestValidateVolumeMounts_SameClaimBothModesRejected(t *testing.T) {
	err := ValidateVolumeMounts([]EnvVolumeMount{
		{ClaimName: "shared", MountPath: "/volume/read", ReadOnly: ptr.To(true)},
		{ClaimName: "shared", MountPath: "/volume/write", ReadOnly: ptr.To(false)},
	}, sandboxPodSpec())
	if err == nil {
		t.Fatal("a claim in both modes must be refused: the Pod would never start")
	}
	for _, want := range []string{"read-only and read-write", "subPath"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q so the user knows the way out; got %q", want, err)
		}
	}
}

func TestValidateVolumeMounts_TemplateHygiene(t *testing.T) {
	t.Run("reserved volume name prefix", func(t *testing.T) {
		spec := sandboxPodSpec()
		spec.Volumes = append(spec.Volumes, corev1.Volume{Name: ReservedVolumeNamePrefix + "sneaky"})
		err := ValidateVolumeMounts(
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/volume/a"}}, spec)
		if err == nil || !strings.Contains(err.Error(), "reserved prefix") {
			t.Fatalf("want the reserved prefix rejected, got %v", err)
		}
	})

	t.Run("recursiveReadOnly is refused", func(t *testing.T) {
		spec := sandboxPodSpec()
		rro := corev1.RecursiveReadOnlyEnabled
		spec.Containers[0].VolumeMounts[0].RecursiveReadOnly = &rro
		err := ValidateVolumeMounts(
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/volume/a"}}, spec)
		if err == nil || !strings.Contains(err.Error(), "recursiveReadOnly") {
			t.Fatalf("want recursiveReadOnly rejected, got %v", err)
		}
	})

	t.Run("nil template spec skips template checks", func(t *testing.T) {
		if err := ValidateVolumeMounts(
			[]EnvVolumeMount{{ClaimName: "a", MountPath: "/mnt/agentbox"}}, nil); err != nil {
			t.Errorf("with no template spec there is nothing to collide with: %v", err)
		}
	})
}

func TestReadOnlyDefeatingFeatures(t *testing.T) {
	privileged := func() *corev1.PodSpec {
		s := sandboxPodSpec()
		s.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: ptr.To(true)}
		return s
	}
	withCap := func(c corev1.Capability) *corev1.PodSpec {
		s := sandboxPodSpec()
		s.Containers[0].SecurityContext = &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{Add: []corev1.Capability{c}},
		}
		return s
	}

	cases := []struct {
		name     string
		spec     *corev1.PodSpec
		wantSub  string
		wantNone bool
	}{
		{name: "clean spec", spec: sandboxPodSpec(), wantNone: true},
		{name: "nil spec", spec: nil, wantNone: true},
		{name: "privileged container", spec: privileged(), wantSub: "is privileged"},
		{name: "SYS_ADMIN", spec: withCap("SYS_ADMIN"), wantSub: "adds capability SYS_ADMIN"},
		{name: "ALL", spec: withCap("ALL"), wantSub: "adds capability ALL"},
		{name: "harmless capability", spec: withCap("IPC_LOCK"), wantNone: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReadOnlyDefeatingFeatures(tc.spec)
			if tc.wantNone {
				if len(got) != 0 {
					t.Errorf("want no findings, got %v", got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "; "), tc.wantSub) {
				t.Errorf("findings %v do not mention %q", got, tc.wantSub)
			}
		})
	}

	t.Run("privileged init container", func(t *testing.T) {
		s := sandboxPodSpec()
		s.InitContainers = []corev1.Container{{
			Name:            "setup",
			SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)},
		}}
		got := ReadOnlyDefeatingFeatures(s)
		if !strings.Contains(strings.Join(got, "; "), `init container "setup" is privileged`) {
			t.Errorf("init containers must be inspected too, got %v", got)
		}
	})

	t.Run("bidirectional propagation", func(t *testing.T) {
		s := sandboxPodSpec()
		bidi := corev1.MountPropagationBidirectional
		s.Containers[0].VolumeMounts[0].MountPropagation = &bidi
		got := ReadOnlyDefeatingFeatures(s)
		if !strings.Contains(strings.Join(got, "; "), "Bidirectional propagation") {
			t.Errorf("want Bidirectional flagged, got %v", got)
		}
	})

	t.Run("hostPath volume", func(t *testing.T) {
		s := sandboxPodSpec()
		s.Volumes = append(s.Volumes, corev1.Volume{
			Name:         "host",
			VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet"}},
		})
		got := ReadOnlyDefeatingFeatures(s)
		if !strings.Contains(strings.Join(got, "; "), "hostPath volume") {
			t.Errorf("want hostPath flagged, got %v", got)
		}
	})

	t.Run("unmasked procMount", func(t *testing.T) {
		s := sandboxPodSpec()
		pm := corev1.UnmaskedProcMount
		s.Containers[0].SecurityContext = &corev1.SecurityContext{ProcMount: &pm}
		got := ReadOnlyDefeatingFeatures(s)
		if !strings.Contains(strings.Join(got, "; "), "procMount=Unmasked") {
			t.Errorf("want procMount flagged, got %v", got)
		}
	})

	t.Run("host namespaces", func(t *testing.T) {
		s := sandboxPodSpec()
		s.HostPID, s.HostIPC = true, true
		got := strings.Join(ReadOnlyDefeatingFeatures(s), "; ")
		if !strings.Contains(got, "hostPID") || !strings.Contains(got, "hostIPC") {
			t.Errorf("want host namespaces flagged, got %q", got)
		}
	})
}

func TestValidateReadOnlyEnforceable(t *testing.T) {
	privilegedSpec := func() *corev1.PodSpec {
		s := sandboxPodSpec()
		s.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: ptr.To(true)}
		return s
	}

	roMount := []EnvVolumeMount{{ClaimName: "ds", MountPath: "/volume/ds"}} // ReadOnly nil == true
	rwMount := []EnvVolumeMount{{ClaimName: "ds", MountPath: "/volume/ds", ReadOnly: ptr.To(false)}}

	t.Run("clean template accepts read-only", func(t *testing.T) {
		if err := ValidateReadOnlyEnforceable(roMount, sandboxPodSpec(), false); err != nil {
			t.Errorf("want accepted, got %v", err)
		}
	})

	t.Run("privileged template refuses read-only", func(t *testing.T) {
		err := ValidateReadOnlyEnforceable(roMount, privilegedSpec(), false)
		if err == nil {
			t.Fatal("want rejection")
		}
		if !strings.Contains(err.Error(), "privileged") {
			t.Errorf("error should name the offending feature, got %q", err)
		}
		if !strings.Contains(err.Error(), AllowUnenforceableReadOnlyVolumesAnnotationKey) {
			t.Errorf("error should name the opt-out annotation, got %q", err)
		}
	})

	// The load-bearing narrowing: a writable mount never claimed a guarantee,
	// so privileged Docker-in-Docker templates keep working.
	t.Run("privileged template accepts writable mounts", func(t *testing.T) {
		if err := ValidateReadOnlyEnforceable(rwMount, privilegedSpec(), false); err != nil {
			t.Errorf("writable mounts must be unaffected by the rule, got %v", err)
		}
	})

	t.Run("mixed: one read-only entry is enough to refuse", func(t *testing.T) {
		mixed := append(append([]EnvVolumeMount{}, rwMount...), roMount[0])
		mixed[1].MountPath = "/volume/other"
		if err := ValidateReadOnlyEnforceable(mixed, privilegedSpec(), false); err == nil {
			t.Error("want rejection when any entry is read-only")
		}
	})

	t.Run("admin opt-out permits it", func(t *testing.T) {
		if err := ValidateReadOnlyEnforceable(roMount, privilegedSpec(), true); err != nil {
			t.Errorf("the annotation must permit it, got %v", err)
		}
	})

	t.Run("no volumes is always fine", func(t *testing.T) {
		if err := ValidateReadOnlyEnforceable(nil, privilegedSpec(), false); err != nil {
			t.Errorf("want accepted, got %v", err)
		}
	})
}

func TestBoolAnnotation(t *testing.T) {
	obj := &SandboxTemplate{}
	if BoolAnnotation(obj, "k") {
		t.Error("absent annotation must be false")
	}
	obj.Annotations = map[string]string{
		"t": "true", "one": "1", "f": "false", "junk": "yes-please", "spaced": "  true  ",
	}
	cases := map[string]bool{"t": true, "one": true, "f": false, "junk": false, "spaced": true, "missing": false}
	for key, want := range cases {
		if got := BoolAnnotation(obj, key); got != want {
			t.Errorf("BoolAnnotation(%q) = %v, want %v", key, got, want)
		}
	}
	if BoolAnnotation(nil, "t") {
		t.Error("nil object must be false")
	}
}
