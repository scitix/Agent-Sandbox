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

	corev1 "k8s.io/api/core/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
)

// PoolAdmitter runs the pool-lifecycle plugin hooks (PreCreatePool /
// PreUpdatePool / PreDeletePool) against a prospective Pool object.
//
// It does NOT write to Kubernetes — the Env Reconciler materialises the
// SandboxPool CR. The Env service calls these methods before patching the
// Env so plugins (e.g. quota reservation) can mutate or reject the request,
// matching the admission semantics of the legacy `/sandboxpools` endpoints.
type PoolAdmitter interface {
	// AdmitCreate runs PreCreatePool plugin hooks. The plugin may mutate
	// candidate.Labels / candidate.Annotations / candidate.Spec; the caller
	// must propagate those mutations back to the EnvClusterMember so the
	// Reconciler renders them onto the eventual Pool CR.
	AdmitCreate(ctx context.Context, candidate *agentsv1alpha1.SandboxPool) *domain.AppError

	// AdmitUpdate runs PreUpdatePool plugin hooks. Returns updated=true
	// when the plugin mutated the candidate.
	AdmitUpdate(ctx context.Context, candidate *agentsv1alpha1.SandboxPool, pods []corev1.Pod) (updated bool, err *domain.AppError)

	// AdmitDelete runs PreDeletePool plugin hooks against the existing Pool.
	AdmitDelete(ctx context.Context, pool *agentsv1alpha1.SandboxPool) *domain.AppError
}

// NoOpPoolAdmitter is the default — no plugins, no admission. Used by
// open-source builds and unit tests that don't exercise the plugin path.
type NoOpPoolAdmitter struct{}

func (NoOpPoolAdmitter) AdmitCreate(_ context.Context, _ *agentsv1alpha1.SandboxPool) *domain.AppError {
	return nil
}
func (NoOpPoolAdmitter) AdmitUpdate(_ context.Context, _ *agentsv1alpha1.SandboxPool, _ []corev1.Pod) (bool, *domain.AppError) {
	return false, nil
}
func (NoOpPoolAdmitter) AdmitDelete(_ context.Context, _ *agentsv1alpha1.SandboxPool) *domain.AppError {
	return nil
}

// pluginPoolAdmitter delegates to PluginManager. A nil manager is treated
// as "no plugins registered" so callers can pass a zero-config instance in
// open-source mode.
type pluginPoolAdmitter struct {
	manager *plugins.PluginManager
}

// NewPoolAdmitter constructs a PoolAdmitter backed by the supplied
// PluginManager. Passing nil yields a no-op admitter.
func NewPoolAdmitter(m *plugins.PluginManager) PoolAdmitter {
	if m == nil {
		return NoOpPoolAdmitter{}
	}
	return &pluginPoolAdmitter{manager: m}
}

func (a *pluginPoolAdmitter) AdmitCreate(ctx context.Context, candidate *agentsv1alpha1.SandboxPool) *domain.AppError {
	return a.manager.PreCreatePool(ctx, candidate)
}

func (a *pluginPoolAdmitter) AdmitUpdate(ctx context.Context, candidate *agentsv1alpha1.SandboxPool, pods []corev1.Pod) (bool, *domain.AppError) {
	return a.manager.PreUpdatePool(ctx, candidate, pods)
}

func (a *pluginPoolAdmitter) AdmitDelete(ctx context.Context, pool *agentsv1alpha1.SandboxPool) *domain.AppError {
	return a.manager.PreDeletePool(ctx, pool)
}
