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
		{in: "Bearer ${e2b.secrets.openai}", want: "Bearer sk-real"},
		{in: "${e2b.secrets.openai}:${e2b.secrets.tok}", want: "sk-real:abc"},
		{in: "Bearer ${e2b.secrets.missing}", wantErr: true},
		// The retired doubled-curly syntax is not a reference any more. It has
		// to pass through untouched rather than half-expand — validation is
		// what rejects it, and it does so before a value ever reaches here.
		{in: "Bearer {{ openai }}", want: "Bearer {{ openai }}"},
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
			Name:      "openai",
			ValueFrom: agentsv1alpha1.SecretKeyRef{Name: "creds", Key: "openai"},
		}},
		Rules: []agentsv1alpha1.InjectionRule{{
			Host:    "api.openai.com",
			Headers: []agentsv1alpha1.HeaderInjection{{Name: "Authorization", Value: "Bearer ${e2b.secrets.openai}"}},
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
	if plan.secrets.CAKeyPEM == "" || plan.secrets.CACertPEM == "" {
		t.Fatal("no CA minted")
	}

	// ...and the sandbox gets nothing but the trust-store overrides. Whatever
	// value a tool inside it reads is an ordinary env var the caller set on the
	// create request; the credential itself never crosses this line.
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
// the CA overrides must ride along with the sandbox's own variables rather than
// arrive in a second call that would wipe them.
func TestMergeInitHook_MergesIntoExistingInitCall(t *testing.T) {
	hooks := []Action{{HTTPPost: &HTTPPostAction{
		Port: envdInitPort,
		Path: "/init",
		Body: map[string]any{"envVars": map[string]string{"USER_VAR": "keep-me"}},
	}}}

	out := mergeInitHook(hooks, "CA-PEM", map[string]string{"SSL_CERT_FILE": "/etc/agbx/ca.pem"})
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
	if env["SSL_CERT_FILE"] != "/etc/agbx/ca.pem" {
		t.Fatal("the CA path override was not merged in")
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

// With nothing to deliver, the caller's own hooks are left alone but an empty
// /init is still appended: envd can be gated on having received one, and a
// sandbox that had nothing to send would otherwise never lift that gate.
func TestMergeInitHook_StillSendsInitWithNothingToDeliver(t *testing.T) {
	hooks := []Action{{Exec: &ExecAction{Command: "true"}}}
	out := mergeInitHook(hooks, "", nil)
	if len(out) != 2 {
		t.Fatalf("expected the caller's hook plus an /init, got %+v", out)
	}
	if out[0].Exec == nil || out[0].Exec.Command != "true" {
		t.Fatalf("the caller's own hook must be preserved and come first: %+v", out[0])
	}
	if out[1].HTTPPost == nil || out[1].HTTPPost.Path != "/init" {
		t.Fatalf("expected an appended /init, got %+v", out[1])
	}
	if len(out[1].HTTPPost.Body) != 0 {
		t.Fatalf("nothing to deliver means an empty body, got %+v", out[1].HTTPPost.Body)
	}
}

// An /init the caller already declared is the carrier; a second one would
// overwrite the first's effect with an empty body.
func TestMergeInitHook_DoesNotDuplicateAnExistingInit(t *testing.T) {
	hooks := []Action{{HTTPPost: &HTTPPostAction{
		Port: envdInitPort, Path: "/init", Body: map[string]any{"envVars": map[string]string{"A": "1"}},
	}}}
	out := mergeInitHook(hooks, "", nil)
	if len(out) != 1 {
		t.Fatalf("expected the existing /init to be reused, got %+v", out)
	}
	if out[0].HTTPPost.Body["envVars"] == nil {
		t.Fatal("the existing body must survive")
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
