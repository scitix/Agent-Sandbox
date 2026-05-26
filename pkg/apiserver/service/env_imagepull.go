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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/dockerconfig"
)

// upsertEnvImagePullSecret materialises the dockerconfigjson Secret backing
// an Env's overrides.imagePullSecret. The Secret name is
// agentsv1alpha1.EnvImagePullSecretName(env.Name); it carries an
// OwnerReference back to the Env so Kubernetes GC cleans it up when the Env
// is deleted.
//
// Upsert semantics: when no Secret exists, create. When one already exists,
// patch its Data with the new dockerconfigjson payload (does NOT touch
// labels / annotations the user may have added out of band).
func (s *k8sSandboxEnvService) upsertEnvImagePullSecret(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	input *gen.ImagePullSecretInput,
) *domain.AppError {
	if input == nil || len(input.Registries) == 0 {
		return nil
	}
	creds := make([]dockerconfig.RegistryCredential, 0, len(input.Registries))
	for _, r := range input.Registries {
		creds = append(creds, dockerconfig.RegistryCredential{
			Registry: r.Registry,
			Username: r.Username,
			Password: r.Password,
		})
	}
	payload, err := dockerconfig.Build(creds)
	if err != nil {
		return domain.NewBadRequest(fmt.Sprintf("invalid imagePullSecret: %v", err))
	}

	secretName := agentsv1alpha1.EnvImagePullSecretName(env.Name)
	key := client.ObjectKey{Namespace: env.Namespace, Name: secretName}
	existing := &corev1.Secret{}
	if err := s.client.Get(ctx, key, existing); err != nil {
		if !k8serrors.IsNotFound(err) {
			return domain.NewInternal(fmt.Sprintf("lookup image pull secret: %v", err), err)
		}
		// Create path — stamp the OwnerRef + payload up front.
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: env.Namespace,
				Labels: map[string]string{
					"agentbox.io/type": "image-pull-secret",
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
			Type: corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: payload,
			},
		}
		if err := s.client.Create(ctx, secret); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				// Lost a race; fall through to the update path on next
				// reconcile / next call.
				return nil
			}
			return domain.NewInternal(fmt.Sprintf("create image pull secret: %v", err), err)
		}
		return nil
	}

	// Update path — only the Data changes; preserve labels / annotations
	// / owner refs as-is (the original Create stamped them correctly, and
	// users may have added their own).
	base := existing.DeepCopy()
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[corev1.DockerConfigJsonKey] = payload
	existing.Type = corev1.SecretTypeDockerConfigJson
	if err := s.client.Patch(ctx, existing, client.MergeFrom(base)); err != nil {
		return domain.NewInternal(fmt.Sprintf("update image pull secret: %v", err), err)
	}
	return nil
}
