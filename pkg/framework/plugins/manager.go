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

package plugins

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework"
)

// PluginManager holds an ordered list of plugins and executes them
// sequentially for each lifecycle hook.
//
// A nil *PluginManager is safe to use — all Run* methods are no-ops.
// This is the expected state for the open-source build (no plugins registered).
type PluginManager struct {
	plugins []Plugin
}

// NewPluginManager creates a PluginManager with the given plugins.
// Returns nil if no plugins are provided, enabling the nil-safe fast path.
func NewPluginManager(plugins ...Plugin) *PluginManager {
	if len(plugins) == 0 {
		return nil
	}
	return &PluginManager{plugins: plugins}
}

// Start invokes Start on every registered plugin in order. If any plugin
// returns an error, Start stops and returns immediately — the caller should
// treat this as a fatal bootstrap failure.
//
// Safe to call on a nil receiver (no-op).
func (m *PluginManager) Start(ctx context.Context, h framework.Handle) error {
	if m == nil {
		return nil
	}
	for _, p := range m.plugins {
		if err := p.Start(ctx, h); err != nil {
			return fmt.Errorf("plugin %q Start: %w", p.Name(), err)
		}
	}
	return nil
}

// PreCreatePool calls PreCreate on every registered plugin in order.
// Returns updated=true if any plugin mutated pool, and the first error
// encountered (short-circuits).
func (m *PluginManager) PreCreatePool(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	if m == nil {
		return false, nil
	}
	updated := false
	for _, p := range m.plugins {
		u, err := p.PreCreatePool(ctx, pool)
		if u {
			updated = true
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "plugin PreCreate failed", "plugin", p.Name())
			return updated, err
		}
	}
	return updated, nil
}

// PreUpdatePool calls PreUpdate on every registered plugin in order.
// Returns updated=true if any plugin mutated newPool, and the first error encountered (short-circuits).
func (m *PluginManager) PreUpdatePool(ctx context.Context, newPool *agentsv1alpha1.SandboxPool, pods []corev1.Pod) (bool, *domain.AppError) {
	if m == nil {
		return false, nil
	}
	updated := false
	for _, p := range m.plugins {
		u, err := p.PreUpdatePool(ctx, newPool, pods)
		if u {
			updated = true
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "plugin PreUpdate failed", "plugin", p.Name())
			return updated, err
		}
	}
	return updated, nil
}

// PreDeletePool calls PreDelete on every registered plugin in order.
// Returns updated=true if any plugin mutated pool, and the first error
// encountered (short-circuits).
func (m *PluginManager) PreDeletePool(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	if m == nil {
		return false, nil
	}
	updated := false
	for _, p := range m.plugins {
		u, err := p.PreDeletePool(ctx, pool)
		if u {
			updated = true
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "plugin PreDelete failed", "plugin", p.Name())
			return updated, err
		}
	}
	return updated, nil
}

// PreCreatePodHooks calls PreCreatePod on every registered plugin in order.
// Returns updated=true if any plugin mutated pod, and the first error
// encountered (short-circuits).
func (m *PluginManager) PreCreatePodHooks(ctx context.Context, pod *corev1.Pod, pool *agentsv1alpha1.SandboxPool) (bool, *domain.AppError) {
	if m == nil {
		return false, nil
	}
	updated := false
	for _, p := range m.plugins {
		u, err := p.PreCreatePod(ctx, pod, pool)
		if u {
			updated = true
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "plugin PreCreatePod failed", "plugin", p.Name())
			return updated, err
		}
	}
	return updated, nil
}
