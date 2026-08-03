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

package poststarthooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func TestExpandCredentialTemplate(t *testing.T) {
	values := map[string]string{"openai": "sk-real", "tok": "abc"}
	for _, tc := range []struct {
		in, want string
		wantErr  bool
	}{
		{in: "Bearer {{ openai }}", want: "Bearer sk-real"},
		{in: "Bearer {{openai}}", want: "Bearer sk-real"},
		{in: "{{ openai }}:{{ tok }}", want: "sk-real:abc"},
		{in: "Bearer {{ missing }}", wantErr: true},
	} {
		got, err := expandCredentialTemplate(tc.in, values)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected an error for an unknown credential", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

func injectPod(annotation string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sbx-pod",
			Namespace: "default",
			Annotations: map[string]string{
				agentsv1alpha1.SandboxIDAnnotationKey:           "sbx-1",
				agentsv1alpha1.SandboxEgressInjectAnnotationKey: annotation,
			},
		},
	}
}

func injectionAnnotation(t *testing.T) string {
	t.Helper()
	si := agentsv1alpha1.SecretInjection{
		Credentials: []agentsv1alpha1.InjectedCredential{{
			Name:        "openai",
			ValueFrom:   agentsv1alpha1.SecretKeyRef{Name: "creds", Key: "openai"},
			ExposeAs:    "OPENAI_API_KEY",
			Placeholder: "agbx_ph_0123456789abcdef",
		}},
		Rules: []agentsv1alpha1.InjectionRule{{
			Host:       "api.openai.com",
			Headers:    []agentsv1alpha1.HeaderInjection{{Name: "Authorization", Value: "Bearer {{ openai }}"}},
			Substitute: []string{"openai"},
		}},
	}
	b, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestPrepareInjection_ResolvesCredentialsAndMintsCA(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"},
		Data:       map[string][]byte{"openai": []byte("sk-real-value")},
	})
	r := &Runner{clientset: cs}

	plan, err := r.prepareInjection(context.Background(), injectPod(injectionAnnotation(t)))
	if err != nil {
		t.Fatalf("prepareInjection: %v", err)
	}
	if plan == nil {
		t.Fatal("no plan produced")
	}

	// The sidecar gets the resolved value...
	if got := plan.secrets.Rules[0].Headers[0].Value; got != "Bearer sk-real-value" {
		t.Fatalf("sidecar header value = %q, want the resolved credential", got)
	}
	if plan.secrets.Substitutions["agbx_ph_0123456789abcdef"] != "sk-real-value" {
		t.Fatalf("decoy is not mapped to the real value: %+v", plan.secrets.Substitutions)
	}
	if plan.secrets.CAKeyPEM == "" || plan.secrets.CACertPEM == "" {
		t.Fatal("no CA minted")
	}

	// ...and the sandbox gets only the decoy.
	if plan.envVars["OPENAI_API_KEY"] != "agbx_ph_0123456789abcdef" {
		t.Fatalf("sandbox env got %q, want the decoy", plan.envVars["OPENAI_API_KEY"])
	}
	for k, v := range plan.envVars {
		if strings.Contains(v, "sk-real-value") {
			t.Fatalf("env var %s leaks the real credential into the sandbox", k)
		}
	}
	// The CA private key must never be part of what the sandbox sees.
	if strings.Contains(plan.caCertPEM, "PRIVATE KEY") {
		t.Fatal("the sandbox-facing bundle contains a private key")
	}
	for _, k := range caEnvVars {
		if plan.envVars[k] != sandboxCABundlePath {
			t.Fatalf("%s = %q, want the CA bundle path", k, plan.envVars[k])
		}
	}
}

func TestPrepareInjection_NoAnnotationIsNoop(t *testing.T) {
	r := &Runner{clientset: fake.NewClientset()}
	plan, err := r.prepareInjection(context.Background(), &corev1.Pod{})
	if err != nil || plan != nil {
		t.Fatalf("plan=%v err=%v, want both nil", plan, err)
	}
}

func TestPrepareInjection_MissingSecretIsAnError(t *testing.T) {
	r := &Runner{clientset: fake.NewClientset()}
	if _, err := r.prepareInjection(context.Background(), injectPod(injectionAnnotation(t))); err == nil {
		t.Fatal("a missing Secret must fail loudly rather than arm an empty credential")
	}
}

func TestPrepareInjection_MissingKeyIsAnError(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"},
		Data:       map[string][]byte{"other": []byte("x")},
	})
	r := &Runner{clientset: cs}
	if _, err := r.prepareInjection(context.Background(), injectPod(injectionAnnotation(t))); err == nil {
		t.Fatal("a missing Secret key must fail loudly")
	}
}

// envd replaces the whole user env-var set on any /init carrying envVars, so
// the CA and decoys must ride along with the sandbox's own variables rather
// than arrive in a second call that would wipe them.
func TestMergeInitHook_MergesIntoExistingInitCall(t *testing.T) {
	hooks := []Action{{HTTPPost: &HTTPPostAction{
		Port: envdInitPort,
		Path: "/init",
		Body: map[string]any{"envVars": map[string]string{"USER_VAR": "keep-me"}},
	}}}

	out := mergeInitHook(hooks, "CA-PEM", map[string]string{"OPENAI_API_KEY": "decoy"})
	if len(out) != 1 {
		t.Fatalf("expected the existing hook to be reused, got %d hooks", len(out))
	}
	body := out[0].HTTPPost.Body
	if body["caBundle"] != "CA-PEM" {
		t.Fatalf("caBundle not merged: %v", body["caBundle"])
	}
	env, _ := body["envVars"].(map[string]string)
	if env["USER_VAR"] != "keep-me" {
		t.Fatal("merging dropped the sandbox's own env var")
	}
	if env["OPENAI_API_KEY"] != "decoy" {
		t.Fatal("decoy was not merged in")
	}
}

func TestMergeInitHook_CreatesHookWhenNoneExists(t *testing.T) {
	out := mergeInitHook(nil, "CA-PEM", map[string]string{"A": "b"})
	if len(out) != 1 || out[0].HTTPPost == nil {
		t.Fatalf("expected a synthesised /init hook, got %+v", out)
	}
	if out[0].HTTPPost.Path != "/init" || out[0].HTTPPost.Port != envdInitPort {
		t.Fatalf("wrong target: %+v", out[0].HTTPPost)
	}
}

func TestMergeInitHook_NothingToDoIsNoop(t *testing.T) {
	hooks := []Action{{Exec: &ExecAction{Command: "true"}}}
	out := mergeInitHook(hooks, "", nil)
	if len(out) != 1 || out[0].Exec == nil {
		t.Fatalf("unexpected mutation: %+v", out)
	}
}

// A non-/init hook must not be hijacked as the carrier.
func TestMergeInitHook_IgnoresOtherHooks(t *testing.T) {
	hooks := []Action{{HTTPPost: &HTTPPostAction{Port: 8080, Path: "/other"}}}
	out := mergeInitHook(hooks, "CA-PEM", nil)
	if len(out) != 2 {
		t.Fatalf("expected a new /init hook alongside the existing one, got %d", len(out))
	}
	if out[0].HTTPPost.Path != "/other" {
		t.Fatal("the unrelated hook was modified")
	}
}
