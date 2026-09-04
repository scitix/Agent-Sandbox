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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func baseRevSpec() *SandboxPoolSpec {
	return &SandboxPoolSpec{
		Replicas:               3,
		PodCreationImagePolicy: PodCreationImagePolicyIdleImage,
		EmbeddedSandboxTemplate: EmbeddedSandboxTemplate{
			IdleImage: "idle:v1",
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "sandbox", Image: "run:v1"}},
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
			},
		},
	}
}

func TestComputeRevisionHash_StableAndDeterministic(t *testing.T) {
	a := ComputeRevisionHash(baseRevSpec())
	b := ComputeRevisionHash(baseRevSpec())
	if a == "" {
		t.Fatal("hash is empty")
	}
	if a != b {
		t.Errorf("hash not deterministic: %q != %q", a, b)
	}
}

func TestComputeRevisionHash_SelfReferenceIgnored(t *testing.T) {
	s := baseRevSpec()
	base := ComputeRevisionHash(s)
	// Stamping the hash label into the template must not change the hash.
	s.Template.Labels = map[string]string{TemplateHashLabelKey: base}
	if got := ComputeRevisionHash(s); got != base {
		t.Errorf("hash label must be excluded: %q != %q", got, base)
	}
}

func TestComputeRevisionHash_SensitiveToPodIdentity(t *testing.T) {
	base := ComputeRevisionHash(baseRevSpec())

	cases := map[string]func(*SandboxPoolSpec){
		"idleImage change": func(s *SandboxPoolSpec) { s.IdleImage = "idle:v2" },
		"delete affinity":  func(s *SandboxPoolSpec) { s.Template.Spec.Affinity = nil },
		"add sidecar": func(s *SandboxPoolSpec) {
			s.Template.Spec.Containers = append(s.Template.Spec.Containers, corev1.Container{Name: "sidecar", Image: "sc:v1"})
		},
		"enable gateway": func(s *SandboxPoolSpec) {
			s.Gateway = &GatewaySpec{Enabled: true}
		},
		"template label": func(s *SandboxPoolSpec) {
			s.Template.Labels = map[string]string{"team": "hisys"}
		},
	}
	for name, mut := range cases {
		s := baseRevSpec()
		mut(s)
		if got := ComputeRevisionHash(s); got == base {
			t.Errorf("%s: hash unchanged (%q), expected different", name, got)
		}
	}
}

func TestComputeRevisionHash_InsensitiveToNonIdentity(t *testing.T) {
	base := ComputeRevisionHash(baseRevSpec())

	cases := map[string]func(*SandboxPoolSpec){
		"replicas":       func(s *SandboxPoolSpec) { s.Replicas = 99 },
		"startupTimeout": func(s *SandboxPoolSpec) { s.DefaultStartupTimeout = &metav1.Duration{Duration: 1} },
		"idleTimeout":    func(s *SandboxPoolSpec) { s.DefaultIdleTimeout = &metav1.Duration{Duration: 1} },
		"maxUnavailable": func(s *SandboxPoolSpec) { v := intstr.FromString("50%"); s.MaxUnavailable = &v },
		"templateName":   func(s *SandboxPoolSpec) { s.TemplateName = "other" },
		// Under the IdleImage policy the idle Pod runs IdleImage, so the running
		// (containers[0]) image must NOT affect the hash — it is resolved live at claim.
		"running image (IdleImage policy)": func(s *SandboxPoolSpec) { s.Template.Spec.Containers[0].Image = "run:v2" },
	}
	for name, mut := range cases {
		s := baseRevSpec()
		mut(s)
		if got := ComputeRevisionHash(s); got != base {
			t.Errorf("%s: hash changed (%q != %q), expected identical", name, got, base)
		}
	}
}

func TestComputeRevisionHash_RunningImageMattersUnderPoolDefault(t *testing.T) {
	// Under PoolDefaultImage the idle Pod runs the template image, so a running
	// image change DOES change identity.
	s := baseRevSpec()
	s.PodCreationImagePolicy = PodCreationImagePolicyPoolDefaultImage
	base := ComputeRevisionHash(s)
	s.Template.Spec.Containers[0].Image = "run:v2"
	if got := ComputeRevisionHash(s); got == base {
		t.Errorf("PoolDefaultImage: running image change must alter hash, got %q", got)
	}
}
