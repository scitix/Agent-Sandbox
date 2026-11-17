// Copyright 2026 ScitiX
// SPDX-License-Identifier: Apache-2.0
package envmember_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envmember"
	"github.com/scitix/agent-sandbox/pkg/controllers/sandboxenv/poolrender"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
)

func acceleratorCatalog() fakeInstProvider {
	entries := map[string]*instancetypeplugin.InstanceType{}
	for name, model := range map[string]string{"gpu-a": "model-a", "gpu-b": "model-b"} {
		reqs := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi"), "nvidia.com/gpu": resource.MustParse("1")}
		entries[name] = &instancetypeplugin.InstanceType{Name: name, BaseResources: corev1.ResourceRequirements{Requests: reqs, Limits: reqs.DeepCopy()}, NodeSelector: map[string]string{"example.com/model": model}}
	}
	return fakeInstProvider{catalog: entries}
}

func namedAccelerator(name string) agentsv1alpha1.EnvClusterMember {
	return agentsv1alpha1.EnvClusterMember{Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 0}, Config: agentsv1alpha1.EnvClusterMemberConfig{InstanceType: name, Multiplier: 1}}
}

func TestAddInstancePlacementFrozenAndNotEditable(t *testing.T) {
	ctx := context.Background()
	tmpl := newTestTemplate()
	tmpl.Spec.Template.Spec.NodeSelector = map[string]string{"example.com/zone": "zone-a"}
	cli := newClient(t, newEnvForPoolOps(), tmpl)
	provider := acceleratorCatalog()
	svc := envmember.New(cli, nil, provider, nil)
	result, err := svc.AddMember(ctx, envTestNamespace, testEnvName, envLocalCluster, namedAccelerator("gpu-a"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != testEnvName+"-8c16gi-1gpu" {
		t.Fatalf("wrong GPU pool name: %s", result.Name)
	}
	stored := &agentsv1alpha1.SandboxEnv{}
	key := client.ObjectKey{Namespace: envTestNamespace, Name: testEnvName}
	if err := cli.Get(ctx, key, stored); err != nil {
		t.Fatal(err)
	}
	member := stored.Spec.Clusters[0].Members[0]
	if member.Config.ScalingGroup != "8c16gi-1gpu" {
		t.Fatal(member.Config.ScalingGroup)
	}
	if member.Config.Annotations[instancetypeplugin.NodeSelectorSnapshotAnnotation] != "" {
		t.Fatal("server snapshot leaked into caller config")
	}
	if member.Spec.Template.Spec.NodeSelector["example.com/model"] != "model-a" || member.Spec.Template.Spec.NodeSelector["example.com/zone"] != "zone-a" {
		t.Fatal("placement not materialized")
	}
	provider.catalog["gpu-a"].NodeSelector["example.com/model"] = "changed-catalog"
	rendered, rerr := poolrender.RenderSandboxPool(poolrender.Inputs{Env: stored, Template: tmpl, Member: member})
	if rerr != nil {
		t.Fatal(rerr)
	}
	if rendered.Spec.Template.Spec.NodeSelector["example.com/model"] != "model-a" {
		t.Fatal("catalog edit retargeted frozen member")
	}
	annotations := maps.Clone(member.Config.Annotations)
	patch := envmember.MemberPoolPatch{MaxReplicas: ptr.To(int32(2)), InstanceType: ptr.To(member.Config.InstanceType), Multiplier: ptr.To(member.Config.Multiplier), InlineResources: member.Config.InlineResources.DeepCopy(), Annotations: &annotations}
	if _, err := svc.UpdateMember(ctx, envTestNamespace, testEnvName, member.Name, envLocalCluster, patch); err != nil {
		t.Fatal(err)
	}
	if err := cli.Get(ctx, key, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Spec.Clusters[0].Members[0].Metadata.Annotations[instancetypeplugin.NodeSelectorSnapshotAnnotation] != member.Metadata.Annotations[instancetypeplugin.NodeSelectorSnapshotAnnotation] {
		t.Fatal("ordinary update removed frozen placement")
	}
	annotations[instancetypeplugin.NodeSelectorSnapshotAnnotation] = `{"example.com/model":"forged"}`
	if _, err := svc.UpdateMember(ctx, envTestNamespace, testEnvName, member.Name, envLocalCluster, patch); err == nil {
		t.Fatal("update forged frozen placement")
	}
}

func TestAddRejectsPlacementForgeryAndMixedModels(t *testing.T) {
	ctx := context.Background()
	svc := envmember.New(newClient(t, newEnvForPoolOps(), newTestTemplate()), nil, acceleratorCatalog(), nil)
	for _, inMetadata := range []bool{false, true} {
		member := namedAccelerator("gpu-a")
		forged := map[string]string{instancetypeplugin.NodeSelectorSnapshotAnnotation: `{"example.com/model":"forged"}`}
		if inMetadata {
			member.Metadata.Annotations = forged
		} else {
			member.Config.Annotations = forged
		}
		if _, err := svc.AddMember(ctx, envTestNamespace, testEnvName, envLocalCluster, member); err == nil || err.Code != domain.ErrCodeBadRequest {
			t.Fatalf("forged placement accepted: %v", err)
		}
	}
	if _, err := svc.AddMember(ctx, envTestNamespace, testEnvName, envLocalCluster, namedAccelerator("gpu-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMember(ctx, envTestNamespace, testEnvName, envLocalCluster, namedAccelerator("gpu-b")); err == nil || !strings.Contains(err.Message, "different frozen instance placement") {
		t.Fatalf("mixed GPU models not rejected clearly: %v", err)
	}
	cpu := memberWithResources(0)
	cpu.Config.InlineResources.Requests[corev1.ResourceCPU] = resource.MustParse("8")
	cpu.Config.InlineResources.Requests[corev1.ResourceMemory] = resource.MustParse("16Gi")
	if result, err := svc.AddMember(ctx, envTestNamespace, testEnvName, envLocalCluster, cpu); err != nil || result.Name != testEnvName+"-8c16gi" {
		t.Fatalf("CPU cannot coexist separately: %v %v", result, err)
	}
}

// Simulate another API request creating a differently constrained member after
// initial validation but before this request's optimistic Env patch succeeds.
type concurrentPlacementClient struct {
	client.Client
	injected bool
}

func (c *concurrentPlacementClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if !c.injected {
		c.injected = true
		current := &agentsv1alpha1.SandboxEnv{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
			return err
		}
		current.Spec.Clusters[0].Members = append(current.Spec.Clusters[0].Members, agentsv1alpha1.EnvClusterMember{Name: "existing-other-quota", Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "8c16gi-1gpu", InlineResources: acceleratorCatalog().catalog["gpu-a"].BaseResources.DeepCopy()}, Metadata: agentsv1alpha1.MemberMetadata{Annotations: map[string]string{instancetypeplugin.NodeSelectorSnapshotAnnotation: `{"example.com/model":"model-a"}`}}})
		if err := c.Update(ctx, current); err != nil {
			return err
		}
		return k8serrors.NewConflict(schema.GroupResource{Group: agentsv1alpha1.GroupVersion.Group, Resource: "sandboxenvs"}, current.Name, errors.New("concurrent member create"))
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func TestAddRechecksModelIsolationOnConflict(t *testing.T) {
	cli := &concurrentPlacementClient{Client: newClient(t, newEnvForPoolOps(), newTestTemplate())}
	svc := envmember.New(cli, nil, acceleratorCatalog(), nil)
	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, namedAccelerator("gpu-b")); err == nil || !strings.Contains(err.Message, "different frozen instance placement") {
		t.Fatalf("concurrent mixed-model add accepted: %v", err)
	}
	stored := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), client.ObjectKey{Namespace: envTestNamespace, Name: testEnvName}, stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Spec.Clusters[0].Members) != 1 || stored.Spec.Clusters[0].Members[0].Name != "existing-other-quota" {
		t.Fatal("rejected concurrent create changed existing members")
	}
}

func TestAddDoesNotMixCPUIntoLegacyGPUGroup(t *testing.T) {
	env := newEnvForPoolOps()
	legacy := memberWithResources(0)
	legacy.Name = "legacy-gpu-pool"
	legacy.Config.ScalingGroup = envTestTwoCPUGroup
	legacy.Config.InlineResources.Requests["nvidia.com/gpu"] = resource.MustParse("1")
	env.Spec.Clusters[0].Members = []agentsv1alpha1.EnvClusterMember{legacy}
	cli := newClient(t, env, newTestTemplate())
	svc := envmember.New(cli, nil, nil, nil)
	if _, err := svc.AddMember(context.Background(), envTestNamespace, testEnvName, envLocalCluster, memberWithResources(0)); err == nil || !strings.Contains(err.Message, "different device resources") {
		t.Fatalf("CPU was allowed into old GPU resource group: %v", err)
	}
	stored := &agentsv1alpha1.SandboxEnv{}
	if err := cli.Get(context.Background(), client.ObjectKey{Namespace: envTestNamespace, Name: testEnvName}, stored); err != nil {
		t.Fatal(err)
	}
	if got := stored.Spec.Clusters[0].Members; len(got) != 1 || got[0].Name != legacy.Name || got[0].Config.ScalingGroup != envTestTwoCPUGroup {
		t.Fatal("legacy identity was migrated")
	}
}
