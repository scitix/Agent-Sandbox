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

package domain

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// The version the platform reports has to be the version the sandbox runs.
//
// E2B's SDK reads this field to decide how it talks to envd, and it reads it in
// three places that matter: `get_metrics()` refuses outright below 0.1.5, disk
// metrics below 0.2.4, and `connect()`/`list()` parse it with PEP 440 — where a
// value it cannot parse is not a wrong answer but an exception on every call.
// The platform used to answer a flat "0.1.0" for that safe-parse reason, which
// is why `get_metrics()` said "You need to update the template to use the new
// SDK" for a sandbox whose envd was 0.9.0.

// v090 is the version the platform currently builds; spelled once so a bump in
// DefaultEnvdVersion does not have to be chased through the expectations.
const v090 = "0.9.0"

func envdPod(template corev1.PodTemplateSpec) agentsv1alpha1.EmbeddedSandboxTemplate {
	return agentsv1alpha1.EmbeddedSandboxTemplate{Template: template}
}

func podWithInits(inits ...corev1.Container) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{Spec: corev1.PodSpec{InitContainers: inits}}
}

func poolWithEnvdImage(image string) *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "ns"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: envdPod(podWithInits(
				corev1.Container{Name: "tini-injector", Image: "registry/tini-static:v0.19.0"},
				corev1.Container{Name: "envd-injector", Image: image},
			)),
		},
	}
}

func TestEnvdVersionForPool_ReadsTheInjectorTag(t *testing.T) {
	// The tag names a BUILD of a version: the platform publishes `0.9.0-2` when
	// envd is 0.9.0 and only the image moved (a changed entrypoint, a new
	// patch). The version is what the SDK compares, so the suffix is dropped
	// rather than reported.
	for _, tc := range []struct {
		image string
		want  string
	}{
		{"registry-ap-southeast.scitix.ai/k8s/navix-agent-sandbox-envd:0.9.0-2", "0.9.0"},
		{"ghcr.io/scitix/agent-sandbox-envd:0.6.1", "0.6.1"},
		{"ghcr.io/scitix/agent-sandbox-envd:v1.2.3", "1.2.3"},
		// A rebuilt image whose entrypoint changed twice: still 0.9.0.
		{"registry/navix-agent-sandbox-envd:0.9.0-1-test", "0.9.0"},
	} {
		if got := EnvdVersionForPool(poolWithEnvdImage(tc.image)); got != tc.want {
			t.Errorf("EnvdVersionForPool(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

func TestEnvdVersionForPool_RecognisesTheInjectorByEitherName(t *testing.T) {
	// Neither half of the convention is declared anywhere, so either one may be
	// what a template uses: a container called `envd-injector`, or an image
	// published as `…-envd`.
	byImage := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: envdPod(podWithInits(
				corev1.Container{Name: "copy-runtime", Image: "ghcr.io/scitix/agent-sandbox-envd:0.9.0"},
			)),
		},
	}
	if got := EnvdVersionForPool(byImage); got != v090 {
		t.Errorf("recognised by image repository, got %q", got)
	}
	byName := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: envdPod(podWithInits(
				corev1.Container{Name: "envd-injector", Image: "registry.internal/tools:0.9.0"},
			)),
		},
	}
	if got := EnvdVersionForPool(byName); got != v090 {
		t.Errorf("recognised by container name, got %q", got)
	}
}

func TestEnvdVersionForPool_FallsBackWhenTheTagIsNotAVersion(t *testing.T) {
	// A tag is not obliged to be a version — `latest`, a git sha, no tag at
	// all. The SDK raises on those, in paths that have nothing to do with
	// metrics, so what it gets is the platform's own envd version rather than
	// whatever was written in the tag.
	for _, image := range []string{
		"ghcr.io/scitix/agent-sandbox-envd:latest",
		"ghcr.io/scitix/agent-sandbox-envd:sha-f4ad6d6",
		"ghcr.io/scitix/agent-sandbox-envd",
	} {
		got := EnvdVersionForPool(poolWithEnvdImage(image))
		if got != DefaultEnvdVersion {
			t.Errorf("EnvdVersionForPool(%q) = %q, want the fallback %q", image, got, DefaultEnvdVersion)
		}
	}
}

func TestEnvdVersionForPool_FallsBackWithNoTemplate(t *testing.T) {
	if got := EnvdVersionForPool(nil); got != DefaultEnvdVersion {
		t.Errorf("nil pool = %q, want %q", got, DefaultEnvdVersion)
	}
	empty := &agentsv1alpha1.SandboxPool{ObjectMeta: metav1.ObjectMeta{Name: "pool-a"}}
	if got := EnvdVersionForPool(empty); got != DefaultEnvdVersion {
		t.Errorf("pool without a template = %q, want %q", got, DefaultEnvdVersion)
	}
	bakedIn := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			EmbeddedSandboxTemplate: envdPod(corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sandbox", Image: "img:1"}}},
			}),
		},
	}
	if got := EnvdVersionForPool(bakedIn); got != DefaultEnvdVersion {
		t.Errorf("envd baked into the image = %q, want %q", got, DefaultEnvdVersion)
	}
}

func TestEnvdVersionForPool_AnnotationWins(t *testing.T) {
	// The escape hatch for a template that does not inject envd from a tagged
	// image at all: say the version, and it is believed over any tag.
	pool := poolWithEnvdImage("ghcr.io/scitix/agent-sandbox-envd:0.6.1")
	pool.Annotations = map[string]string{EnvdVersionAnnotation: "0.9.0-2"}
	if got := EnvdVersionForPool(pool); got != v090 {
		t.Errorf("annotation should win, got %q", got)
	}
	// An annotation that is not a version does not become one: it falls through
	// to the tag, which is a fact rather than a hope.
	pool.Annotations[EnvdVersionAnnotation] = "the-new-one"
	if got := EnvdVersionForPool(pool); got != "0.6.1" {
		t.Errorf("unparseable annotation should fall through to the tag, got %q", got)
	}
}

func TestEnvdVersionForEnv_UsesTheMemberSnapshot(t *testing.T) {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: "env-a"},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Clusters: []agentsv1alpha1.EnvClusterSpec{{
				Members: []agentsv1alpha1.EnvClusterMember{{
					Name: "env-a-1c1gi",
					Spec: agentsv1alpha1.SandboxPoolSpec{
						EmbeddedSandboxTemplate: envdPod(podWithInits(
							corev1.Container{Name: "envd-injector", Image: "registry/navix-agent-sandbox-envd:0.9.0-2"},
						)),
					},
				}},
			}},
		},
	}
	if got := EnvdVersionForEnv(env); got != v090 {
		t.Errorf("EnvdVersionForEnv = %q, want %s", got, v090)
	}
	if got := EnvdVersionForEnv(nil); got != DefaultEnvdVersion {
		t.Errorf("nil env = %q, want %q", got, DefaultEnvdVersion)
	}
}
