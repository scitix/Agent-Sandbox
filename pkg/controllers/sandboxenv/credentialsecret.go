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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// credentialsCondition carries what reconcileCredentialSecret found, so
// syncStatus can report it inside the same status patch.
//
// evaluated=false means "no credentials declared": the condition is removed
// rather than reported, keeping it off Envs that do not use injection.
type credentialsCondition struct {
	evaluated bool
	status    metav1.ConditionStatus
	reason    string
	message   string
}

// reconcileCredentialSecret guarantees the Env's own credential Secret exists,
// then reports whether every declared credential currently resolves.
//
// Ownership is split deliberately and must stay split:
//
//   - the Reconciler owns the Secret's *existence* — it creates an empty one
//     when missing (so an out-of-band delete cannot leave the Env pointing at
//     nothing) and never touches Data;
//   - the API write path owns the Secret's *Data* — it writes each credential's
//     value and drops keys for credentials no longer declared.
//
// The Reconciler must not prune keys: its cache can lag a write, so it would
// occasionally delete a key the API had just stored for a credential the cached
// Env does not know about yet. Two writers to one object is the failure mode
// that ate the images-catalog ConfigMap; keep the halves apart.
//
// An existing Secret with a missing or empty key is reported, not repaired —
// values live only here, so the Reconciler has nothing to reconstruct them
// from. Claim-time resolution fails closed on the same three cases (see the
// poststarthooks injection runner), and this condition is where that shows up
// before a sandbox is ever claimed.
func (r *SandboxEnvReconciler) reconcileCredentialSecret(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
) (credentialsCondition, error) {
	creds := declaredCredentials(env)
	if len(creds) == 0 {
		return credentialsCondition{}, nil
	}

	managedName := agentsv1alpha1.EnvSecretInjectionName(env.Name)
	if slicesContainsManaged(creds, managedName) {
		if err := r.ensureManagedCredentialSecret(ctx, env, managedName); err != nil {
			return credentialsCondition{}, err
		}
	}

	// Read each referenced Secret once; a credential may point at one the
	// caller manages instead of the Env's own.
	seen := map[string]*corev1.Secret{}
	for _, c := range creds {
		ref := c.ValueFrom
		if ref.Name == "" || ref.Key == "" {
			return unresolvable(fmt.Sprintf("credential %q has no valueFrom", c.Name)), nil
		}
		secret, ok := seen[ref.Name]
		if !ok {
			secret = &corev1.Secret{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: ref.Name}, secret); err != nil {
				if !apierrors.IsNotFound(err) {
					return credentialsCondition{}, err
				}
				secret = nil
			}
			seen[ref.Name] = secret
		}
		switch {
		case secret == nil:
			return unresolvable(fmt.Sprintf("secret %s/%s does not exist (credential %q)",
				env.Namespace, ref.Name, c.Name)), nil
		case len(secret.Data[ref.Key]) == 0:
			return unresolvable(fmt.Sprintf("secret %s/%s has no value under key %q (credential %q); "+
				"re-enter the credential value, or point valueFrom at a Secret you manage",
				env.Namespace, ref.Name, ref.Key, c.Name)), nil
		}
	}

	return credentialsCondition{
		evaluated: true,
		status:    metav1.ConditionTrue,
		reason:    "AllCredentialsResolved",
		message:   fmt.Sprintf("all %d declared credential(s) resolve", len(creds)),
	}, nil
}

// ensureManagedCredentialSecret creates the Env's credential Secret when it is
// missing. Data is left empty on purpose — only the API write path fills it.
func (r *SandboxEnvReconciler) ensureManagedCredentialSecret(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	name string,
) error {
	existing := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: name}, existing)
	if err == nil {
		return nil // exists — its Data belongs to the API write path
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: env.Namespace,
			Labels: map[string]string{
				"agentbox.io/type": "secret-injection",
				"agentbox.io/env":  env.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         agentsv1alpha1.GroupVersion.String(),
				Kind:               agentsv1alpha1.SandboxEnvOwnerKind,
				Name:               env.Name,
				UID:                env.UID,
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
		Type: corev1.SecretTypeOpaque,
	}
	if err := r.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func declaredCredentials(env *agentsv1alpha1.SandboxEnv) []agentsv1alpha1.InjectedCredential {
	if env == nil || env.Spec.Overrides == nil || env.Spec.Overrides.NetworkPolicy == nil ||
		env.Spec.Overrides.NetworkPolicy.SecretInjection == nil {
		return nil
	}
	return env.Spec.Overrides.NetworkPolicy.SecretInjection.Credentials
}

func slicesContainsManaged(creds []agentsv1alpha1.InjectedCredential, managedName string) bool {
	for i := range creds {
		if creds[i].ValueFrom.Name == managedName {
			return true
		}
	}
	return false
}

func unresolvable(message string) credentialsCondition {
	return credentialsCondition{
		evaluated: true,
		status:    metav1.ConditionFalse,
		reason:    "CredentialUnresolved",
		message:   message,
	}
}
