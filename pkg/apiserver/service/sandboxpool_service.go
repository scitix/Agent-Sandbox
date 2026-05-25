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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// SandboxPoolService defines business operations for SandboxPools.
//
// Template / overrides reconciliation moved off this interface: Pool now stores
// the rendered result only. The Env Reconciler renders new members from the
// source SandboxTemplate at create time, and the Env-level
// `POST /envs/{name}/sync-template` endpoint re-renders existing members. See
// pkg/controllers/sandboxenv/poolsync.go.
type SandboxPoolService interface {
	List(ctx context.Context, namespace, team, user string) ([]gen.SandboxPool, *domain.AppError)
	Get(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError)
}

type k8sSandboxPoolService struct {
	client        client.Client
	clientset     kubernetes.Interface   // nil means Event-based diagnostics disabled for Get
	pluginManager *plugins.PluginManager // nil means no plugins (open-source mode)
}

// NewSandboxPoolService creates a new SandboxPoolService backed by the given K8s client.
// clientset may be nil (disables Event-based diagnostics in Get).
// pluginManager may be nil (disables lifecycle plugins — open-source mode).
func NewSandboxPoolService(c client.Client, clientset kubernetes.Interface, pluginManager *plugins.PluginManager) SandboxPoolService {
	return &k8sSandboxPoolService{
		client:        c,
		clientset:     clientset,
		pluginManager: pluginManager,
	}
}

// createImagePullSecret materialises the given credentials as a kubernetes.io/dockerconfigjson
// Secret named ips-{poolName}, with OwnerReference pointing at the pool.
// buildAvailableTemplatesDetail lists templates visible to the calling
// team/user and returns a compact summary for attaching to 404 errors when a
// requested template does not exist. Best-effort: list errors return nil so
// the 404 path is never worsened by enrichment.
// List returns all SandboxPools in the given namespace, with per-pod diagnostics
// derived from Pod YAML only (no Kubernetes Events API calls).
// When team and user are non-empty, only pools with matching labels are returned.
func (s *k8sSandboxPoolService) List(ctx context.Context, namespace, team, user string) ([]gen.SandboxPool, *domain.AppError) {
	listOpts := []client.ListOption{client.InNamespace(namespace)}
	if team != "" && user != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			agentsv1alpha1.LabelTeam: team,
			agentsv1alpha1.LabelUser: user,
		})
	}
	poolList := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, poolList, listOpts...); err != nil {
		return nil, domain.NewInternal(err.Error(), err)
	}

	items := make([]gen.SandboxPool, 0, len(poolList.Items))
	for i := range poolList.Items {
		p := &poolList.Items[i]
		items = append(items, poolToGen(ctx, p, nil))
	}
	// Sort by name for consistent ordering (especially important for tests)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// Get returns a single SandboxPool.
func (s *k8sSandboxPoolService) Get(ctx context.Context, namespace, name string) (*gen.SandboxPool, *domain.AppError) {
	pool := &agentsv1alpha1.SandboxPool{}
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("pool %q not found", name))
		}
		return nil, domain.NewInternal(err.Error(), err)
	}

	var tmpl *agentsv1alpha1.SandboxTemplate
	if templateName := pool.Annotations[agentsv1alpha1.SandboxPoolTemplateNameAnnotationKey]; templateName != "" {
		t := &agentsv1alpha1.SandboxTemplate{}
		if err := s.client.Get(ctx, client.ObjectKey{Name: templateName}, t); err == nil {
			tmpl = t
		}
	}

	result := poolToGen(ctx, pool, tmpl)
	return &result, nil
}

// ---------------------------------------------------------------------------
// private helpers
// ---------------------------------------------------------------------------

// poolToGen converts a CRD SandboxPool (plus optional source template) to the gen
// wire shape. The CRD spec is intentionally not exposed; instead we project the
// fields the API documents (replicas, default timeouts, template reference,
// computed CPU/Memory, SpecYaml for diff) into gen.SandboxPool.
func poolToGen(ctx context.Context, pool *agentsv1alpha1.SandboxPool, tmpl *agentsv1alpha1.SandboxTemplate) gen.SandboxPool {
	spec := gen.SandboxPoolSpec{
		Replicas: pool.Spec.Replicas,
	}
	if pool.Spec.TemplateName != "" {
		spec.TemplateName = ptr.To(pool.Spec.TemplateName)
	}
	if pool.Spec.DefaultStartupTimeout != nil {
		spec.DefaultStartupTimeout = ptr.To(pool.Spec.DefaultStartupTimeout.Duration.String())
	}
	if pool.Spec.DefaultIdleTimeout != nil {
		spec.DefaultIdleTimeout = ptr.To(pool.Spec.DefaultIdleTimeout.Duration.String())
	}
	if pool.Spec.PodCreationImagePolicy != "" {
		policy := gen.SandboxPoolSpecPodCreationImagePolicy(pool.Spec.PodCreationImagePolicy)
		spec.PodCreationImagePolicy = &policy
	}

	status := gen.SandboxPoolStatus{
		IdleReplicas:            ptr.To(pool.Status.IdleReplicas),
		UnavailableIdleReplicas: ptr.To(pool.Status.UnavailableIdleReplicas),
		RunningReplicas:         ptr.To(pool.Status.RunningReplicas),
		StartingReplicas:        ptr.To(pool.Status.StartingReplicas),
		StoppingReplicas:        ptr.To(pool.Status.StoppingReplicas),
		FailedReplicas:          ptr.To(pool.Status.FailedReplicas),
		PendingRequests:         ptr.To(pool.Status.PendingRequests),
	}
	if pool.Status.Phase != "" {
		phase := gen.SandboxPoolStatusPhase(pool.Status.Phase)
		status.Phase = &phase
	}

	specYaml := embeddedTemplateToYAML(pool.Spec.EmbeddedSandboxTemplate)
	createdAt := pool.CreationTimestamp.UTC()
	result := gen.SandboxPool{
		Name:            pool.Name,
		Namespace:       pool.Namespace,
		Spec:            spec,
		Status:          status,
		Team:            ptr.To(pool.Labels[agentsv1alpha1.LabelTeam]),
		User:            ptr.To(pool.Labels[agentsv1alpha1.LabelUser]),
		TemplateVersion: ptr.To(pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]),
		SpecYaml:        ptr.To(specYaml),
		CreatedAt:       &createdAt,
	}
	// Pool no longer caches overrides — SandboxEnv.spec.overrides is the
	// source of truth and Pool.spec.embedded already reflects the rendered
	// result. result.Overrides stays nil; clients should read overrides
	// from the owning Env.
	if tmpl != nil {
		if v := tmpl.Annotations[agentsv1alpha1.SandboxTemplateDocsAnnotationKey]; v != "" {
			result.PoolDocs = ptr.To(v)
		}
	}
	if pool.Spec.Template != nil {
		cpu, memory, err := utilresource.SumContainerResources(pool.Spec.Template)
		if err != nil {
			log.FromContext(ctx).V(1).Info("failed to compute pool resources", "pool", pool.Name, "error", err)
		} else {
			result.Cpu = ptr.To(cpu.String())
			result.Memory = ptr.To(memory.String())
		}
	}
	// Surface the Pool's owning SandboxEnv (Phase 1 adoption stamps a non-
	// controlling OwnerReference). Used by the dashboard for reverse navigation
	// from Pool → Env.
	for i := range pool.OwnerReferences {
		ref := &pool.OwnerReferences[i]
		if ref.Kind == agentsv1alpha1.SandboxEnvOwnerKind &&
			strings.HasPrefix(ref.APIVersion, agentsv1alpha1.GroupVersion.Group+"/") {
			result.OwningEnv = ptr.To(ref.Name)
			break
		}
	}
	return result
}

// embeddedTemplateToYAML serializes the EmbeddedSandboxTemplate fields (idleImage, runtimes,
// reservation, template) to a YAML string for use in the SyncTemplate diff view.
// Returns an empty string if marshalling fails.
func embeddedTemplateToYAML(emb agentsv1alpha1.EmbeddedSandboxTemplate) string {
	type diffable struct {
		IdleImage string                              `json:"idleImage,omitempty"`
		Runtimes  []agentsv1alpha1.SandboxRuntimeSpec `json:"runtimes,omitempty"`
		Template  *corev1.PodTemplateSpec             `json:"template,omitempty"`
	}
	d := diffable{
		IdleImage: emb.IdleImage,
		Runtimes:  emb.Runtimes,
		Template:  emb.Template,
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		return ""
	}
	return string(b)
}

// buildPoolFromInput constructs a minimal SandboxPool from the given input.
// validatePoolSpec checks cross-field constraints on the pool spec.
// Call after all input fields have been applied (Create or Update).
