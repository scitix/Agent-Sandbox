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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func envDeclaring(creds ...agentsv1alpha1.InjectedCredential) *agentsv1alpha1.SandboxEnv {
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: "myenv", Namespace: "default", UID: types.UID("uid-1")},
	}
	if len(creds) > 0 {
		env.Spec.Overrides = &agentsv1alpha1.EnvOverridesSpec{
			NetworkPolicy: &agentsv1alpha1.SandboxNetworkPolicy{
				SecretInjection: &agentsv1alpha1.SecretInjection{Credentials: creds},
			},
		}
	}
	return env
}

func managedCred(name string) agentsv1alpha1.InjectedCredential {
	return agentsv1alpha1.InjectedCredential{
		Name:      name,
		ValueFrom: agentsv1alpha1.SecretKeyRef{Name: agentsv1alpha1.EnvSecretInjectionName("myenv"), Key: name},
	}
}

func newCredReconciler(t *testing.T, objs ...client.Object) *SandboxEnvReconciler {
	t.Helper()
	scheme := newReconcileTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &SandboxEnvReconciler{Client: c, Scheme: scheme, LocalClusterID: testLocalCluster}
}

func getSecret(t *testing.T, r *SandboxEnvReconciler, name string) *corev1.Secret {
	t.Helper()
	sec := &corev1.Secret{}
	if err := r.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, sec); err != nil {
		t.Fatalf("get secret %q: %v", name, err)
	}
	return sec
}

// The Secret's existence is the Reconciler's job, so an Env that declares a
// managed credential always has something to point at — even if the value has
// not been typed yet.
func TestReconcileCredentialSecret_CreatesMissingSecret(t *testing.T) {
	env := envDeclaring(managedCred("navix"))
	r := newCredReconciler(t, env)

	cond, err := r.reconcileCredentialSecret(context.Background(), env)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	sec := getSecret(t, r, agentsv1alpha1.EnvSecretInjectionName("myenv"))
	if len(sec.Data) != 0 {
		t.Fatalf("the Reconciler must not invent values: %v", sec.Data)
	}
	if len(sec.OwnerReferences) != 1 || sec.OwnerReferences[0].Name != "myenv" {
		t.Fatalf("Secret must be owned by the Env: %+v", sec.OwnerReferences)
	}
	// Existence alone is not resolvability: the key is still empty.
	if !cond.evaluated || cond.status != metav1.ConditionFalse {
		t.Fatalf("an empty key must report unresolvable, got %+v", cond)
	}
}

// Data belongs to the API write path; a reconcile must never rewrite it.
func TestReconcileCredentialSecret_LeavesStoredValuesAlone(t *testing.T) {
	env := envDeclaring(managedCred("navix"))
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default", Name: agentsv1alpha1.EnvSecretInjectionName("myenv")},
		Data: map[string][]byte{"navix": []byte("nvx-real"), "stale": []byte("keep-me")},
	}
	r := newCredReconciler(t, env, sec)

	cond, err := r.reconcileCredentialSecret(context.Background(), env)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := getSecret(t, r, agentsv1alpha1.EnvSecretInjectionName("myenv"))
	if string(got.Data["navix"]) != "nvx-real" {
		t.Fatal("stored credential was modified")
	}
	// Pruning undeclared keys is the API's job: the Reconciler's cache can lag a
	// write, so deleting here would eventually drop a just-stored credential.
	if _, ok := got.Data["stale"]; !ok {
		t.Fatal("the Reconciler must not prune keys")
	}
	if !cond.evaluated || cond.status != metav1.ConditionTrue {
		t.Fatalf("a stored credential must report resolvable, got %+v", cond)
	}
}

// A credential pointing at a Secret the caller manages is reported, not created.
func TestReconcileCredentialSecret_ReportsMissingCallerOwnedSecret(t *testing.T) {
	env := envDeclaring(agentsv1alpha1.InjectedCredential{
		Name:      "navix",
		ValueFrom: agentsv1alpha1.SecretKeyRef{Name: "mine", Key: "tok"},
	})
	r := newCredReconciler(t, env)

	cond, err := r.reconcileCredentialSecret(context.Background(), env)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !cond.evaluated || cond.status != metav1.ConditionFalse {
		t.Fatalf("missing caller-owned Secret must report unresolvable, got %+v", cond)
	}
	if err := r.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "mine"}, &corev1.Secret{}); err == nil {
		t.Fatal("the Reconciler must not create Secrets it does not own")
	}
}

// An Env that declares no credentials carries no condition at all, so removing
// the last credential clears the verdict instead of freezing it.
func TestReconcileCredentialSecret_NoCredentialsIsNotEvaluated(t *testing.T) {
	env := envDeclaring()
	r := newCredReconciler(t, env)

	cond, err := r.reconcileCredentialSecret(context.Background(), env)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if cond.evaluated {
		t.Fatalf("no credentials should leave the condition unset, got %+v", cond)
	}

	env.Status.Conditions = []metav1.Condition{{
		Type:               agentsv1alpha1.SandboxEnvConditionCredentialsResolvable,
		Status:             metav1.ConditionFalse,
		Reason:             "CredentialUnresolved",
		LastTransitionTime: metav1.Now(),
	}}
	setCredentialsResolvableCondition(env, cond)
	for _, c := range env.Status.Conditions {
		if c.Type == agentsv1alpha1.SandboxEnvConditionCredentialsResolvable {
			t.Fatal("a stale CredentialsResolvable condition must be removed")
		}
	}
}
