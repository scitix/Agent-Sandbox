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

package managedagent

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// agentOnThePlatformDefault declares no image and no hands: everything it runs
// with comes from the deployment.
func agentOnThePlatformDefault() *agentsv1alpha1.ManagedAgent {
	return &agentsv1alpha1.ManagedAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "agentbox-system"},
		Spec: agentsv1alpha1.ManagedAgentSpec{
			Runtime: agentsv1alpha1.ManagedAgentRuntime{
				Default: "claude-code",
				ClaudeCode: &agentsv1alpha1.ClaudeCodeRuntime{
					CredentialsRef: agentsv1alpha1.SecretKeySelector{
						Name: "agentbox-brain-fresh-credentials", Key: "CLAUDE_CODE_API_KEY",
					},
				},
			},
		},
	}
}

func reconcilerFor(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	sch := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	if err := agentsv1alpha1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().
		WithScheme(sch).
		WithObjects(objs...).
		WithStatusSubresource(&agentsv1alpha1.ManagedAgent{}).
		Build()
	return &Reconciler{
		Client:            c,
		Scheme:            sch,
		ProxyService:      "agentbox-dashboard-proxy.agentbox-system:9005",
		DefaultBrainImage: agentsv1alpha1.ManagedAgentImage{Repository: "reg/brain", Tag: "v1"},
		DefaultHands:      defaultHands(),
	}, c
}

func reconcile(t *testing.T, r *Reconciler, ma *agentsv1alpha1.ManagedAgent) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ma.Name, Namespace: ma.Namespace},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

// The pod is rendered from the defaults, so the status has to describe them.
// Reporting the raw spec instead would leave an agent that says nothing about its
// sandbox supply while happily using one.
func TestReconcileReportsThePlatformDefaultAsTheSupply(t *testing.T) {
	ma := agentOnThePlatformDefault()
	r, c := reconcilerFor(t, ma)
	reconcile(t, r, ma)

	var got agentsv1alpha1.ManagedAgent
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(ma), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Hands == nil {
		t.Fatal("status.hands is empty; the default was not reported")
	}
	if got.Status.Hands.Source != handsSourcePlatformDefault {
		t.Errorf("source = %q, want %q", got.Status.Hands.Source, handsSourcePlatformDefault)
	}
	if got.Status.Hands.EnvName != defaultEnvName {
		t.Errorf("envName = %q, want the default's", got.Status.Hands.EnvName)
	}
	if !got.Status.Hands.Ready {
		t.Error("hands not ready on a fully configured default")
	}

	// The spec itself must be untouched: the default is re-resolved per reconcile
	// precisely so a deployment can re-point it later.
	if got.Spec.Hands.External != nil || got.Spec.Image.Repository != "" {
		t.Error("the default was written back onto the spec")
	}
}

// An agent that declares its own supply must be reported as such, or an operator
// reading the status cannot tell which agents follow the platform.
func TestReconcileReportsADeclaredSupplyAsTheAgentsOwn(t *testing.T) {
	ma := agentOnThePlatformDefault()
	ma.Spec.Hands = agentsv1alpha1.ManagedAgentHands{
		EnvRef: &agentsv1alpha1.HandsEnvRef{Name: "tenant-env", Image: "reg/sbx:1"},
	}
	r, c := reconcilerFor(t, ma)
	reconcile(t, r, ma)

	var got agentsv1alpha1.ManagedAgent
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(ma), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Hands.Source != handsSourceAgent {
		t.Errorf("source = %q, want %q", got.Status.Hands.Source, handsSourceAgent)
	}
	if got.Status.Hands.EnvName != "tenant-env" {
		t.Errorf("envName = %q, want the agent's own", got.Status.Hands.EnvName)
	}
}

// With no default published, an agent that declares nothing is not silently
// broken: HandsReady says so, and the Brain is still deployed so its own error
// names what is missing.
func TestReconcileWithoutAnyHandsReportsNotReady(t *testing.T) {
	ma := agentOnThePlatformDefault()
	r, c := reconcilerFor(t, ma)
	r.DefaultHands = nil
	reconcile(t, r, ma)

	var got agentsv1alpha1.ManagedAgent
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(ma), &got); err != nil {
		t.Fatal(err)
	}
	for _, cond := range got.Status.Conditions {
		if cond.Type == agentsv1alpha1.ManagedAgentConditionHandsReady {
			if cond.Status != metav1.ConditionFalse || cond.Reason != "NoHands" {
				t.Errorf("HandsReady = %s/%s, want False/NoHands", cond.Status, cond.Reason)
			}
		}
	}
	var dep appsv1.Deployment
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: BrainName(ma.Name), Namespace: ma.Namespace,
	}, &dep); err != nil {
		t.Fatalf("the Brain was not deployed: %v", err)
	}
}

// Rotating the platform's sandbox key has to roll the pod. The credential is
// mounted from a Secret nothing else on the agent references, so if it is left out
// of the checksum the new value never reaches the running Brain and the failure is
// silent — tool calls keep failing against the old key.
func TestReconcileRollsThePodWhenTheDefaultCredentialRotates(t *testing.T) {
	ma := agentOnThePlatformDefault()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-sandbox", Namespace: ma.Namespace},
		Data:       map[string][]byte{"E2B_API_KEY": []byte("agbx_one")},
	}
	r, c := reconcilerFor(t, ma, secret)
	reconcile(t, r, ma)

	depKey := types.NamespacedName{Name: BrainName(ma.Name), Namespace: ma.Namespace}
	var before appsv1.Deployment
	if err := c.Get(context.Background(), depKey, &before); err != nil {
		t.Fatal(err)
	}
	first := before.Spec.Template.Annotations[ConfigChecksumAnnotation]
	if first == "" {
		t.Fatal("no config checksum was rendered")
	}

	secret.Data["E2B_API_KEY"] = []byte("agbx_two")
	if err := c.Update(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	reconcile(t, r, ma)

	var after appsv1.Deployment
	if err := c.Get(context.Background(), depKey, &after); err != nil {
		t.Fatal(err)
	}
	if got := after.Spec.Template.Annotations[ConfigChecksumAnnotation]; got == first {
		t.Error("the checksum did not change; a rotated platform credential would not reach the pod")
	}
}
