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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// resolveInjectedCredentialRefs points every credential that was given a value
// inline at the Env's own credential Secret, so the caller only has to name the
// credential and type its value.
//
// One Secret per Env, one key per credential — not one Secret per credential.
// The reference is filled in before the Env is written; the Secret itself is
// materialised right after (see upsertEnvSecretInjection), the same ordering
// imagePullSecret uses so the OwnerRef has an Env to point at.
//
// values is keyed by credential name and carries what the caller typed. It never
// reaches the CRD.
func resolveInjectedCredentialRefs(overrides *agentsv1alpha1.EnvOverridesSpec, envName string, values map[string]string) *domain.AppError {
	if overrides == nil || overrides.NetworkPolicy == nil || overrides.NetworkPolicy.SecretInjection == nil {
		return nil
	}
	secretName := agentsv1alpha1.EnvSecretInjectionName(envName)
	creds := overrides.NetworkPolicy.SecretInjection.Credentials
	for i := range creds {
		c := &creds[i]
		_, hasValue := values[c.Name]
		hasRef := c.ValueFrom.Name != "" && c.ValueFrom.Key != ""

		switch {
		case hasValue && hasRef:
			return domain.NewBadRequest(fmt.Sprintf(
				"credential %q sets both value and valueFrom; supply one — value stores it for you, "+
					"valueFrom points at a Secret you manage", c.Name))
		case hasValue:
			c.ValueFrom = agentsv1alpha1.SecretKeyRef{Name: secretName, Key: c.Name}
		case hasRef:
			// Caller manages the Secret themselves; leave it alone.
		default:
			// No value now: either the credential already has one stored (an
			// edit that did not re-type it — the field is write-only, so the UI
			// cannot send it back), or it is new and nothing was supplied. The
			// managed reference is filled in either way; whether a value is
			// actually there is checked when the Secret is upserted.
			c.ValueFrom = agentsv1alpha1.SecretKeyRef{Name: secretName, Key: c.Name}
		}
	}
	return nil
}

// preflightInjectedCredentials rejects a write whose credentials could not be
// materialised — before the Env itself is written.
//
// The Secret can only be created after the Env exists, because its OwnerRef
// needs the Env's UID; so the write order has to be Env-then-Secret, and a
// failure at the Secret step used to leave the Env referencing a credential
// that was never stored. That is not a hypothetical: `value` is write-only, so
// an edit that does not re-type a credential sends no value at all, and the
// Env would be saved pointing at a key nobody ever wrote. Checking first makes
// the realistic failure atomic — nothing is persisted and the caller is told
// what to supply.
//
// A credential passes when the caller supplied a value now, or the managed
// Secret already holds a non-empty one. Credentials pointing at a Secret the
// caller manages are their business, not ours.
func (s *k8sSandboxEnvService) preflightInjectedCredentials(
	ctx context.Context,
	namespace, envName string,
	overrides *agentsv1alpha1.EnvOverridesSpec,
	values map[string]string,
) *domain.AppError {
	if overrides == nil || overrides.NetworkPolicy == nil || overrides.NetworkPolicy.SecretInjection == nil {
		return nil
	}
	managed := agentsv1alpha1.EnvSecretInjectionName(envName)
	creds := overrides.NetworkPolicy.SecretInjection.Credentials

	var stored *corev1.Secret
	for i := range creds {
		c := &creds[i]
		if c.ValueFrom.Name != managed && c.ValueFrom.Name != "" {
			continue // caller-managed Secret
		}
		if v, ok := values[c.Name]; ok && v != "" {
			continue
		}
		if stored == nil {
			stored = &corev1.Secret{}
			if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: managed}, stored); err != nil {
				if !k8serrors.IsNotFound(err) {
					return domain.NewInternal(fmt.Sprintf("lookup credential secret: %v", err), err)
				}
				stored.Data = nil
			}
		}
		key := c.ValueFrom.Key
		if key == "" {
			key = c.Name
		}
		if len(stored.Data[key]) == 0 {
			return domain.NewBadRequest(fmt.Sprintf(
				"credential %q has no value; supply one, or point valueFrom at a Secret you manage", c.Name))
		}
	}
	return nil
}

// upsertEnvSecretInjection materialises the Env's credential Secret.
//
// Update semantics matter here because `value` is write-only: an edit that only
// renames a rule will not re-send the credentials, so a wholesale replace of
// Data would wipe every stored value. Instead:
//
//   - credentials carrying a new value overwrite their key;
//   - credentials without one must already have a key, else the caller is told
//     to supply a value rather than silently getting an empty credential;
//   - keys for credentials no longer declared are dropped, so removing a
//     credential from the Env actually removes the material.
func (s *k8sSandboxEnvService) upsertEnvSecretInjection(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	values map[string]string,
) *domain.AppError {
	si := injectionOf(env)
	if si == nil {
		return nil
	}

	// Only credentials pointing at the managed Secret are ours to fill.
	managed := agentsv1alpha1.EnvSecretInjectionName(env.Name)
	wanted := make([]string, 0, len(si.Credentials))
	for _, c := range si.Credentials {
		if c.ValueFrom.Name == managed {
			wanted = append(wanted, c.Name)
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	key := client.ObjectKey{Namespace: env.Namespace, Name: managed}
	existing := &corev1.Secret{}
	err := s.client.Get(ctx, key, existing)
	switch {
	case err != nil && !k8serrors.IsNotFound(err):
		return domain.NewInternal(fmt.Sprintf("lookup credential secret: %v", err), err)

	case err != nil: // create
		data := map[string][]byte{}
		for _, name := range wanted {
			v, ok := values[name]
			if !ok || v == "" {
				return domain.NewBadRequest(fmt.Sprintf(
					"credential %q has no value; supply one, or point valueFrom at a Secret you manage", name))
			}
			data[name] = []byte(v)
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managed,
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
			Data: data,
		}
		if cErr := s.client.Create(ctx, secret); cErr != nil {
			return domain.NewInternal(fmt.Sprintf("create credential secret: %v", cErr), cErr)
		}
		return nil

	default: // update — merge, never replace
		base := existing.DeepCopy()
		if existing.Data == nil {
			existing.Data = map[string][]byte{}
		}
		for _, name := range wanted {
			if v, ok := values[name]; ok && v != "" {
				existing.Data[name] = []byte(v)
				continue
			}
			if _, stored := existing.Data[name]; !stored {
				return domain.NewBadRequest(fmt.Sprintf(
					"credential %q has no value; supply one, or point valueFrom at a Secret you manage", name))
			}
		}
		for k := range existing.Data {
			if !slices.Contains(wanted, k) {
				delete(existing.Data, k)
			}
		}
		if pErr := s.client.Patch(ctx, existing, client.MergeFrom(base)); pErr != nil {
			return domain.NewInternal(fmt.Sprintf("update credential secret: %v", pErr), pErr)
		}
		return nil
	}
}

// credentialDigests returns the first 8 hex characters of each stored
// credential's SHA-256, keyed by credential name. It lets a caller see that a
// value is configured, and notice when it changes, without the value ever being
// returned.
func (s *k8sSandboxEnvService) credentialDigests(ctx context.Context, env *agentsv1alpha1.SandboxEnv) map[string]string {
	si := injectionOf(env)
	if si == nil {
		return nil
	}
	out := map[string]string{}
	cache := map[string]*corev1.Secret{}
	for _, c := range si.Credentials {
		if c.ValueFrom.Name == "" || c.ValueFrom.Key == "" {
			continue
		}
		sec, seen := cache[c.ValueFrom.Name]
		if !seen {
			sec = &corev1.Secret{}
			if err := s.client.Get(ctx, client.ObjectKey{Namespace: env.Namespace, Name: c.ValueFrom.Name}, sec); err != nil {
				sec = nil
			}
			cache[c.ValueFrom.Name] = sec
		}
		if sec == nil {
			continue
		}
		if v, ok := sec.Data[c.ValueFrom.Key]; ok && len(v) > 0 {
			sum := sha256.Sum256(v)
			out[c.Name] = hex.EncodeToString(sum[:])[:8]
		}
	}
	return out
}

func injectionOf(env *agentsv1alpha1.SandboxEnv) *agentsv1alpha1.SecretInjection {
	if env == nil || env.Spec.Overrides == nil || env.Spec.Overrides.NetworkPolicy == nil {
		return nil
	}
	return env.Spec.Overrides.NetworkPolicy.SecretInjection
}
