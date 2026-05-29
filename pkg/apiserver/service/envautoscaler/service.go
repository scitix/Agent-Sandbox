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

// Package envautoscaler serves the Env-scoped autoscaler-config CRUD
// surface. Routes onto `/v1/sandboxenvs/{name}/autoscaling/*`.
//
// Mirrors the envmember package shape: the autoscaler config lives on the
// SandboxEnv spec and the SandboxEnvReconciler picks up changes via its
// regular Watch. Group lifecycle is member-driven — a group is created when
// a member declaring its ScalingGroup is added and garbage-collected by the
// reconciler once unreferenced — so this service exposes read/update/delete
// of groups but no standalone create.
//
// Method names carry an Autoscaling / AutoscalingGroup suffix so the
// interface can be embedded into SandboxEnvService alongside MemberPool
// CRUD without name conflicts.
package envautoscaler

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// AutoscalerService is the Env-scoped autoscaling-config surface.
//
// The per-env Enabled master switch has been removed — each
// EnvAutoscalingGroup carries its own Enabled bit so groups can be
// toggled independently. Update the bit via UpdateAutoscalingGroup with
// GroupPatch.Enabled set.
type AutoscalerService interface {
	// GetAutoscaling returns the env's full autoscaling spec. nil-spec is
	// normalised to a zero EnvAutoscalingSpec so clients can always read
	// .groups without nil-checks.
	GetAutoscaling(ctx context.Context, namespace, envName string) (*gen.EnvAutoscalingSpec, *domain.AppError)

	// Groups are created automatically when a member declaring the matching
	// ScalingGroup is added (see envmember.AddMember) and garbage-collected
	// by the Env reconciler once no member references them; there is no
	// standalone "add group" entry point.
	//
	// UpdateAutoscalingGroup applies a patch to the named group. Only the
	// non-nil patch fields are mutated; policy fields are replaced
	// wholesale when supplied. Returns 404 if no group matches name.
	UpdateAutoscalingGroup(ctx context.Context, namespace, envName, groupName string, patch GroupPatch) (*gen.EnvAutoscalingGroup, *domain.AppError)
	// DeleteAutoscalingGroup removes the named group. 404 if not present.
	DeleteAutoscalingGroup(ctx context.Context, namespace, envName, groupName string) *domain.AppError
	// ListAutoscalingGroups returns every group on the env. Empty slice
	// when autoscaling has never been configured.
	ListAutoscalingGroups(ctx context.Context, namespace, envName string) ([]gen.EnvAutoscalingGroup, *domain.AppError)
	// GetAutoscalingGroup returns one group by name or 404.
	GetAutoscalingGroup(ctx context.Context, namespace, envName, groupName string) (*gen.EnvAutoscalingGroup, *domain.AppError)
}

// GroupPatch is the editable subset of EnvAutoscalingGroup. Pointer fields
// disambiguate "leave unchanged" from "explicit zero/empty".
//
// Enabled / MinReplicas / MaxReplicas: nil = unchanged, non-nil = replace.
// ScaleUpPolicy / ScaleDownPolicy: nil = unchanged; non-nil = REPLACE the
// entire policy (callers must echo back any fields they want to preserve).
type GroupPatch struct {
	Enabled         *bool
	MinReplicas     *int32
	MaxReplicas     *int32
	ScaleUpPolicy   *gen.PoolScaleUpPolicy
	ScaleDownPolicy *gen.PoolScaleDownPolicy
}

// New constructs the default Service implementation backed by the K8s
// client. The autoscaler config CRUD does not currently invoke any plugin
// hooks (unlike MemberPool CRUD, which runs PreCreate/Update/DeletePool);
// adding plugin admission later is a constructor-only change.
func New(c client.Client) AutoscalerService {
	return &k8sService{client: c}
}

type k8sService struct {
	client client.Client
}

func (s *k8sService) GetAutoscaling(ctx context.Context, namespace, envName string) (*gen.EnvAutoscalingSpec, *domain.AppError) {
	env, appErr := s.fetchEnv(ctx, namespace, envName)
	if appErr != nil {
		return nil, appErr
	}
	out := SpecToGen(env.Spec.Autoscaling)
	if out == nil {
		// Normalise so the caller doesn't have to handle nil — an env
		// that's never had autoscaling looks identical to one that's
		// been explicitly disabled with no groups.
		out = &gen.EnvAutoscalingSpec{}
	}
	return out, nil
}

func (s *k8sService) UpdateAutoscalingGroup(ctx context.Context, namespace, envName, groupName string, patch GroupPatch) (*gen.EnvAutoscalingGroup, *domain.AppError) {
	if groupName == "" {
		return nil, domain.NewBadRequest("groupName is required")
	}
	if appErr := validatePolicy(patch.ScaleUpPolicy); appErr != nil {
		return nil, appErr
	}
	var updated agentsv1alpha1.EnvAutoscalingGroup
	if appErr := s.patchSpec(ctx, namespace, envName, func(spec *agentsv1alpha1.SandboxEnvSpec) *domain.AppError {
		if spec.Autoscaling == nil {
			return domain.NewNotFound(fmt.Sprintf("autoscaling group %q not found in env %q", groupName, envName))
		}
		idx := -1
		for i := range spec.Autoscaling.Groups {
			if spec.Autoscaling.Groups[i].Name == groupName {
				idx = i
				break
			}
		}
		if idx < 0 {
			return domain.NewNotFound(fmt.Sprintf("autoscaling group %q not found in env %q", groupName, envName))
		}
		g := &spec.Autoscaling.Groups[idx]
		if patch.Enabled != nil {
			g.Enabled = *patch.Enabled
		}
		if patch.MinReplicas != nil {
			v := *patch.MinReplicas
			g.MinReplicas = &v
		}
		if patch.MaxReplicas != nil {
			v := *patch.MaxReplicas
			g.MaxReplicas = &v
		}
		if patch.ScaleUpPolicy != nil {
			g.ScaleUpPolicy = scaleUpPolicyFromGen(patch.ScaleUpPolicy)
		}
		if patch.ScaleDownPolicy != nil {
			g.ScaleDownPolicy = scaleDownPolicyFromGen(patch.ScaleDownPolicy)
		}
		updated = *g.DeepCopy()
		return nil
	}); appErr != nil {
		return nil, appErr
	}
	out := GroupToGen(updated)
	return &out, nil
}

func (s *k8sService) DeleteAutoscalingGroup(ctx context.Context, namespace, envName, groupName string) *domain.AppError {
	if groupName == "" {
		return domain.NewBadRequest("groupName is required")
	}
	return s.patchSpec(ctx, namespace, envName, func(spec *agentsv1alpha1.SandboxEnvSpec) *domain.AppError {
		if spec.Autoscaling == nil {
			return domain.NewNotFound(fmt.Sprintf("autoscaling group %q not found in env %q", groupName, envName))
		}
		out := spec.Autoscaling.Groups[:0]
		found := false
		for _, g := range spec.Autoscaling.Groups {
			if g.Name == groupName {
				found = true
				continue
			}
			out = append(out, g)
		}
		if !found {
			return domain.NewNotFound(fmt.Sprintf("autoscaling group %q not found in env %q", groupName, envName))
		}
		spec.Autoscaling.Groups = append([]agentsv1alpha1.EnvAutoscalingGroup(nil), out...)
		return nil
	})
}

func (s *k8sService) ListAutoscalingGroups(ctx context.Context, namespace, envName string) ([]gen.EnvAutoscalingGroup, *domain.AppError) {
	env, appErr := s.fetchEnv(ctx, namespace, envName)
	if appErr != nil {
		return nil, appErr
	}
	if env.Spec.Autoscaling == nil {
		return []gen.EnvAutoscalingGroup{}, nil
	}
	out := make([]gen.EnvAutoscalingGroup, 0, len(env.Spec.Autoscaling.Groups))
	for _, g := range env.Spec.Autoscaling.Groups {
		out = append(out, GroupToGen(g))
	}
	return out, nil
}

func (s *k8sService) GetAutoscalingGroup(ctx context.Context, namespace, envName, groupName string) (*gen.EnvAutoscalingGroup, *domain.AppError) {
	if groupName == "" {
		return nil, domain.NewBadRequest("groupName is required")
	}
	env, appErr := s.fetchEnv(ctx, namespace, envName)
	if appErr != nil {
		return nil, appErr
	}
	if env.Spec.Autoscaling != nil {
		for _, g := range env.Spec.Autoscaling.Groups {
			if g.Name == groupName {
				out := GroupToGen(g)
				return &out, nil
			}
		}
	}
	return nil, domain.NewNotFound(fmt.Sprintf("autoscaling group %q not found in env %q", groupName, envName))
}

// fetchEnv reads the SandboxEnv with friendly NotFound mapping.
func (s *k8sService) fetchEnv(ctx context.Context, namespace, envName string) (*agentsv1alpha1.SandboxEnv, *domain.AppError) {
	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: envName}, env); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}
	return env, nil
}

// patchSpec is the shared retry-on-conflict shell every mutating method
// uses. mutator receives a writable pointer to the spec; AppError
// returns from the mutator are bubbled out preserving their code.
func (s *k8sService) patchSpec(ctx context.Context, namespace, envName string, mutator func(*agentsv1alpha1.SandboxEnvSpec) *domain.AppError) *domain.AppError {
	key := types.NamespacedName{Namespace: namespace, Name: envName}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		if appErr := mutator(&current.Spec); appErr != nil {
			return appErr
		}
		return s.client.Patch(ctx, current, client.MergeFrom(base))
	})
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*domain.AppError); ok {
		return appErr
	}
	if k8serrors.IsNotFound(err) {
		return domain.NewNotFound(fmt.Sprintf("sandbox env %q not found in namespace %s", envName, namespace))
	}
	return domain.NewInternal(err.Error(), err)
}

// validatePolicy rejects obviously-invalid ScaleUp modes the OpenAPI spec
// can't catch. Mirrors the same guard the legacy validateEnvAutoscaling
// applied at the env-level Update path.
func validatePolicy(p *gen.PoolScaleUpPolicy) *domain.AppError {
	if p == nil || p.Mode == nil {
		return nil
	}
	mode := agentsv1alpha1.PoolScaleUpMode(*p.Mode)
	switch mode {
	case agentsv1alpha1.PoolScaleUpModeConservative,
		agentsv1alpha1.PoolScaleUpModeDefault,
		agentsv1alpha1.PoolScaleUpModeAggressive:
		return nil
	}
	return domain.NewBadRequest(fmt.Sprintf("scaleUpPolicy.mode %q is invalid", *p.Mode))
}
