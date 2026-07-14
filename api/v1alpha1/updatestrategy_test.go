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

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func envWithOverrideStrategy(s *EnvUpdateStrategy) *SandboxEnv {
	return &SandboxEnv{Spec: SandboxEnvSpec{Overrides: &EnvOverridesSpec{UpdateStrategy: s}}}
}

func memberWithStrategy(s *EnvUpdateStrategy) EnvClusterMember {
	return EnvClusterMember{Config: EnvClusterMemberConfig{UpdateStrategy: s}}
}

func TestResolveAutoUpdate_Inheritance(t *testing.T) {
	cases := []struct {
		name   string
		env    *SandboxEnv
		member EnvClusterMember
		want   bool
	}{
		{"default true", &SandboxEnv{}, EnvClusterMember{}, true},
		{"env false", envWithOverrideStrategy(&EnvUpdateStrategy{AutoUpdate: ptr.To(false)}), EnvClusterMember{}, false},
		{"member overrides env", envWithOverrideStrategy(&EnvUpdateStrategy{AutoUpdate: ptr.To(false)}), memberWithStrategy(&EnvUpdateStrategy{AutoUpdate: ptr.To(true)}), true},
		{"member false over env true", envWithOverrideStrategy(&EnvUpdateStrategy{AutoUpdate: ptr.To(true)}), memberWithStrategy(&EnvUpdateStrategy{AutoUpdate: ptr.To(false)}), false},
		{"member nil field falls back to env", envWithOverrideStrategy(&EnvUpdateStrategy{AutoUpdate: ptr.To(false)}), memberWithStrategy(&EnvUpdateStrategy{}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveAutoUpdate(tc.env, tc.member); got != tc.want {
				t.Errorf("ResolveAutoUpdate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveMaxUnavailable_Inheritance(t *testing.T) {
	twenty := intstr.FromString("20%")
	fifty := intstr.FromString("50%")
	three := intstr.FromInt32(3)

	cases := []struct {
		name   string
		env    *SandboxEnv
		member EnvClusterMember
		want   intstr.IntOrString
	}{
		{"default 20%", &SandboxEnv{}, EnvClusterMember{}, twenty},
		{"env 50%", envWithOverrideStrategy(&EnvUpdateStrategy{MaxUnavailable: &fifty}), EnvClusterMember{}, fifty},
		{"member overrides env", envWithOverrideStrategy(&EnvUpdateStrategy{MaxUnavailable: &fifty}), memberWithStrategy(&EnvUpdateStrategy{MaxUnavailable: &three}), three},
		{"member nil field falls back to env", envWithOverrideStrategy(&EnvUpdateStrategy{MaxUnavailable: &fifty}), memberWithStrategy(&EnvUpdateStrategy{}), fifty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveMaxUnavailable(tc.env, tc.member); got != tc.want {
				t.Errorf("ResolveMaxUnavailable = %v, want %v", got, tc.want)
			}
		})
	}
}
