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

package envcommon

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	utilresource "github.com/scitix/agent-sandbox/pkg/utils/resource"
)

// PoolToGen converts a CRD SandboxPool to the gen wire shape. The CRD spec
// is intentionally not exposed; instead we project the fields the API
// documents (replicas, default timeouts, template reference, computed
// CPU/Memory, SpecYaml for diff) into gen.SandboxPool.
//
// Pool is no longer a user-facing object — the only callers are the
// env-scoped Pool CRUD endpoints in pkg/apiserver/service/envmember, which
// project pools they read off the K8s API server before returning them to
// the dashboard. There is no longer a top-level /sandboxpools service.
func PoolToGen(ctx context.Context, pool *agentsv1alpha1.SandboxPool) gen.SandboxPool {
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
		ScalingGroup:    ptr.To(pool.Labels[agentsv1alpha1.LabelScalingGroup]),
		TemplateVersion: ptr.To(pool.Annotations[agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey]),
		SpecYaml:        ptr.To(specYaml),
		CreatedAt:       &createdAt,
	}
	if len(pool.Spec.Template.Spec.Containers) > 0 {
		cpu, memory, err := utilresource.SumContainerResources(&pool.Spec.Template)
		if err != nil {
			log.FromContext(ctx).V(1).Info("failed to compute pool resources", "pool", pool.Name, "error", err)
		} else {
			result.Cpu = ptr.To(cpu.String())
			result.Memory = ptr.To(memory.String())
		}
	}
	// Surface the Pool's owning SandboxEnv. Used by the dashboard for
	// reverse navigation from Pool → Env.
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

// embeddedTemplateToYAML serialises the EmbeddedSandboxTemplate fields
// (idleImage, runtimes, template) to a YAML string for use in the
// SyncTemplate diff view. Returns an empty string if marshalling fails.
func embeddedTemplateToYAML(emb agentsv1alpha1.EmbeddedSandboxTemplate) string {
	type diffable struct {
		IdleImage string                              `json:"idleImage,omitempty"`
		Runtimes  []agentsv1alpha1.SandboxRuntimeSpec `json:"runtimes,omitempty"`
		Template  corev1.PodTemplateSpec              `json:"template,omitempty"`
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
