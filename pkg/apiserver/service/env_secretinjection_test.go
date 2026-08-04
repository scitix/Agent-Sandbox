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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func envWithCreds(name string, credNames ...string) *agentsv1alpha1.SandboxEnv {
	creds := make([]agentsv1alpha1.InjectedCredential, 0, len(credNames))
	for _, c := range credNames {
		creds = append(creds, agentsv1alpha1.InjectedCredential{Name: c})
	}
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
	}
	env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
		NetworkPolicy: &agentsv1alpha1.SandboxNetworkPolicy{
			SecretInjection: &agentsv1alpha1.SecretInjection{Credentials: creds},
		},
	}
	return env
}

func newEnvSvc(t *testing.T, objs ...client.Object) *k8sSandboxEnvService {
	t.Helper()
	sch := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(sch); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(sch); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(sch).WithObjects(objs...).Build()
	return &k8sSandboxEnvService{client: c}
}

// A caller that types only a name and a value must end up with a working
// reference — that is the whole point of not making them create a Secret.
func TestResolveInjectedCredentialRefs_FillsManagedRef(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	if err := resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", map[string]string{"openai": "sk-1"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	vf := env.Spec.Overrides.NetworkPolicy.SecretInjection.Credentials[0].ValueFrom
	if vf.Name != agentsv1alpha1.EnvSecretInjectionName("myenv") || vf.Key != "openai" {
		t.Fatalf("valueFrom = %+v, want the Env's own credential Secret keyed by the credential name", vf)
	}
}

// Supplying both is ambiguous about which one wins, so it is refused rather
// than silently picking one.
func TestResolveInjectedCredentialRefs_RejectsBoth(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	env.Spec.Overrides.NetworkPolicy.SecretInjection.Credentials[0].ValueFrom =
		agentsv1alpha1.SecretKeyRef{Name: "mine", Key: "k"}
	if err := resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", map[string]string{"openai": "sk-1"}); err == nil {
		t.Fatal("expected value+valueFrom to be rejected")
	}
}

// Pointing at your own Secret must survive untouched.
func TestResolveInjectedCredentialRefs_KeepsCallerOwnedRef(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	env.Spec.Overrides.NetworkPolicy.SecretInjection.Credentials[0].ValueFrom =
		agentsv1alpha1.SecretKeyRef{Name: "mine", Key: "k"}
	if err := resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", nil); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if vf := env.Spec.Overrides.NetworkPolicy.SecretInjection.Credentials[0].ValueFrom; vf.Name != "mine" {
		t.Fatalf("caller-owned ref was overwritten: %+v", vf)
	}
}

func TestUpsertEnvSecretInjection_CreatesOneSecretForTheEnv(t *testing.T) {
	env := envWithCreds("myenv", "openai", "hub")
	_ = resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", map[string]string{"openai": "sk-1", "hub": "h-1"})
	s := newEnvSvc(t)

	if err := s.upsertEnvSecretInjection(context.Background(), env, map[string]string{"openai": "sk-1", "hub": "h-1"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var sec corev1.Secret
	if err := s.client.Get(context.Background(), client.ObjectKey{
		Namespace: "default", Name: agentsv1alpha1.EnvSecretInjectionName("myenv")}, &sec); err != nil {
		t.Fatalf("secret not created: %v", err)
	}
	if string(sec.Data["openai"]) != "sk-1" || string(sec.Data["hub"]) != "h-1" {
		t.Fatalf("both credentials should share one Secret: %v", sec.Data)
	}
	if len(sec.OwnerReferences) != 1 {
		t.Fatal("Secret must be owned by the Env so it is garbage-collected with it")
	}
}

// `value` is write-only, so an edit that only renames a rule sends no values at
// all. Replacing Data wholesale there would wipe every stored credential.
func TestUpsertEnvSecretInjection_EditWithoutValuesKeepsThem(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	_ = resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", nil)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: agentsv1alpha1.EnvSecretInjectionName("myenv"), Namespace: "default"},
		Data:       map[string][]byte{"openai": []byte("sk-stored")},
	}
	s := newEnvSvc(t, existing)

	if err := s.upsertEnvSecretInjection(context.Background(), env, nil); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var sec corev1.Secret
	_ = s.client.Get(context.Background(), client.ObjectKey{
		Namespace: "default", Name: agentsv1alpha1.EnvSecretInjectionName("myenv")}, &sec)
	if string(sec.Data["openai"]) != "sk-stored" {
		t.Fatalf("stored value was lost on an edit that sent none: %q", sec.Data["openai"])
	}
}

// Dropping a credential from the Env should take its material with it.
func TestUpsertEnvSecretInjection_RemovesDroppedCredential(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	_ = resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", nil)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: agentsv1alpha1.EnvSecretInjectionName("myenv"), Namespace: "default"},
		Data:       map[string][]byte{"openai": []byte("sk-stored"), "gone": []byte("old")},
	}
	s := newEnvSvc(t, existing)

	if err := s.upsertEnvSecretInjection(context.Background(), env, nil); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var sec corev1.Secret
	_ = s.client.Get(context.Background(), client.ObjectKey{
		Namespace: "default", Name: agentsv1alpha1.EnvSecretInjectionName("myenv")}, &sec)
	if _, still := sec.Data["gone"]; still {
		t.Fatal("a credential removed from the Env kept its stored value")
	}
	if string(sec.Data["openai"]) != "sk-stored" {
		t.Fatal("the surviving credential lost its value")
	}
}

// A brand-new credential with no value would otherwise be armed as an empty
// string and fail against the upstream with a confusing 401.
func TestUpsertEnvSecretInjection_NewCredentialNeedsAValue(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	_ = resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", nil)
	s := newEnvSvc(t)

	if err := s.upsertEnvSecretInjection(context.Background(), env, nil); err == nil {
		t.Fatal("expected a new credential without a value to be refused")
	}
}

func TestCredentialDigests_NeverReturnsTheValue(t *testing.T) {
	env := envWithCreds("myenv", "openai")
	_ = resolveInjectedCredentialRefs(env.Spec.Overrides, "myenv", nil)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: agentsv1alpha1.EnvSecretInjectionName("myenv"), Namespace: "default"},
		Data:       map[string][]byte{"openai": []byte("sk-super-secret")},
	}
	s := newEnvSvc(t, existing)

	d := s.credentialDigests(context.Background(), env)
	if len(d["openai"]) != 8 {
		t.Fatalf("digest = %q, want 8 hex chars", d["openai"])
	}
	if d["openai"] == "sk-super-secret" {
		t.Fatal("digest leaked the value")
	}
}
