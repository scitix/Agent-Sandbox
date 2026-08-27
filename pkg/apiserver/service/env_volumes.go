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
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// VolumeConfig gates and constrains Env-level PersistentVolumeClaim mounts.
type VolumeConfig struct {
	// Enabled is the kill switch. When false a non-empty overrides.volumes is
	// rejected outright — not merely hidden in the dashboard. The failure mode
	// this feature can produce is an agent with write access to a user's
	// dataset, so the gate has to stop writes, not just UI.
	Enabled bool

	// AllowedRuntimeClasses lists runtimeClassName values whose CSI
	// passthrough has been verified. Empty (the default) permits only the
	// cluster's default runtime, i.e. a Template that leaves runtimeClassName
	// unset. A sandbox running under a VM-isolated runtime reaches the backing
	// filesystem through a different path than a plain container does, and an
	// unverified one silently yields an empty directory or a Pod that never
	// starts.
	AllowedRuntimeClasses []string

	// DisplayNameLabels is an ordered list of PVC label keys consulted to
	// derive a human-readable volume name; the first non-empty value wins.
	// Empty means fall back to the claim name. Deliberately configurable
	// rather than hardcoded: the label vocabulary belongs to whichever storage
	// platform is deployed, not to this codebase.
	DisplayNameLabels []string
}

// validateEnvVolumes runs every check that needs more than the override block
// itself: the feature gate, the resolved Template, and the live claims.
//
// It is called from Create and Update before anything is written, mirroring
// preflightInjectedCredentials — refuse before writing rather than accept a
// spec the reconciler will then decline to converge on.
func (s *k8sSandboxEnvService) validateEnvVolumes(
	ctx context.Context,
	namespace string,
	templateName string,
	o *agentsv1alpha1.EnvOverridesSpec,
) *domain.AppError {
	if o == nil || len(o.Volumes) == 0 {
		return nil
	}
	if !s.volumeCfg.Enabled {
		return domain.NewBadRequest(
			"overrides.volumes is not enabled on this deployment; " +
				"mounting existing PersistentVolumeClaims into sandboxes is disabled")
	}

	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if templateName == "" {
		return domain.NewBadRequest("env.spec.templateRef.name is empty")
	}
	if err := s.client.Get(ctx, client.ObjectKey{Name: templateName}, tmpl); err != nil {
		if k8serrors.IsNotFound(err) {
			return domain.NewNotFound(fmt.Sprintf("source template %q not found", templateName))
		}
		return domain.NewInternal(err.Error(), err)
	}
	podSpec := &tmpl.Spec.Template.Spec

	if err := agentsv1alpha1.ValidateVolumeMounts(o.Volumes, podSpec); err != nil {
		return domain.NewBadRequest(fmt.Sprintf("invalid overrides.volumes: %v", err))
	}

	if err := s.validateRuntimeClass(podSpec.RuntimeClassName); err != nil {
		return err
	}

	allowUnenforceable := agentsv1alpha1.BoolAnnotation(
		tmpl, agentsv1alpha1.AllowUnenforceableReadOnlyVolumesAnnotationKey)
	if err := agentsv1alpha1.ValidateReadOnlyEnforceable(o.Volumes, podSpec, allowUnenforceable); err != nil {
		return domain.NewBadRequest(fmt.Sprintf("invalid overrides.volumes: %v", err))
	}

	return s.preflightVolumeClaims(ctx, namespace, o.Volumes)
}

// validateRuntimeClass refuses a Template whose runtime has not been verified
// to pass the backing filesystem through.
func (s *k8sSandboxEnvService) validateRuntimeClass(rc *string) *domain.AppError {
	if rc == nil || *rc == "" {
		// The cluster default runtime; this is the verified path.
		return nil
	}
	if slices.Contains(s.volumeCfg.AllowedRuntimeClasses, *rc) {
		return nil
	}
	allowed := "only the cluster default runtime (runtimeClassName unset)"
	if len(s.volumeCfg.AllowedRuntimeClasses) > 0 {
		allowed = fmt.Sprintf("the cluster default runtime, or one of: %s",
			strings.Join(s.volumeCfg.AllowedRuntimeClasses, ", "))
	}
	return domain.NewBadRequest(fmt.Sprintf(
		"overrides.volumes is not supported for templates using runtimeClassName %q: "+
			"filesystem passthrough for that runtime has not been verified on this deployment. "+
			"Supported: %s", *rc, allowed))
}

// preflightVolumeClaims requires every referenced claim to exist and be Bound
// in the Env's namespace.
//
// Reads go through the uncached reader on purpose. The manager's delegating
// client starts an informer lazily per type on first access — including on Get —
// so a single cached read here would begin caching every PersistentVolumeClaim
// in the cluster for the lifetime of the process. Per-namespace claim counts are
// small, so a direct read is cheaper than the cache it would build.
func (s *k8sSandboxEnvService) preflightVolumeClaims(
	ctx context.Context,
	namespace string,
	vols []agentsv1alpha1.EnvVolumeMount,
) *domain.AppError {
	reader := s.volumeReader()
	seen := map[string]struct{}{}
	for _, v := range vols {
		if _, done := seen[v.ClaimName]; done {
			continue
		}
		seen[v.ClaimName] = struct{}{}

		pvc := &corev1.PersistentVolumeClaim{}
		key := client.ObjectKey{Namespace: namespace, Name: v.ClaimName}
		if err := reader.Get(ctx, key, pvc); err != nil {
			if k8serrors.IsNotFound(err) {
				return domain.NewBadRequest(fmt.Sprintf(
					"persistentvolumeclaim %q does not exist in namespace %q",
					v.ClaimName, namespace))
			}
			return domain.NewInternal(err.Error(), err)
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			return domain.NewBadRequest(fmt.Sprintf(
				"persistentvolumeclaim %q in namespace %q is %s, not Bound",
				v.ClaimName, namespace, pvc.Status.Phase))
		}
	}
	return nil
}

// volumeReader returns the uncached reader when one was wired in, falling back
// to the cached client so unit tests and embedders that do not supply one still
// work. The fallback is a correctness-preserving convenience, not a supported
// production configuration — see the informer note on preflightVolumeClaims.
func (s *k8sSandboxEnvService) volumeReader() client.Reader {
	if s.apiReader != nil {
		return s.apiReader
	}
	return s.client
}
