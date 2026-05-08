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

package cluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(objs ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	b := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		b = b.WithRuntimeObjects(o)
	}
	return b
}

var testEntries = []ClusterEntry{
	{
		ID:   "cluster-a",
		Name: "Cluster A",
		URL:  "https://cluster-a.example.com",
		Gateway: &GatewayConfig{
			NativeURL: "https://native.cluster-a.internal",
			DataURL:   "https://data.cluster-a.internal",
			Headers:   map[string]string{"X-Token": "secret"},
		},
	},
	{
		ID:   "cluster-b",
		Name: "Cluster B",
		URL:  "https://cluster-b.example.com",
	},
}

func objectKey(ns, name string) client.ObjectKey {
	return types.NamespacedName{Namespace: ns, Name: name}
}

func TestWriteClusterConfig_EmptyEntriesIsNoOp(t *testing.T) {
	c := newFakeClient().Build()
	ctx := context.Background()

	if err := WriteClusterConfig(ctx, c, "agentbox-system", "clusters-config", ClusterConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ConfigMap must NOT have been created.
	cm := &corev1.ConfigMap{}
	err := c.Get(ctx, objectKey("agentbox-system", "clusters-config"), cm)
	if err == nil {
		t.Fatal("expected ConfigMap to be absent, but it exists")
	}
}

func TestWriteClusterConfig_CreatesConfigMap(t *testing.T) {
	c := newFakeClient().Build()
	ctx := context.Background()

	if err := WriteClusterConfig(ctx, c, "agentbox-system", "clusters-config", ClusterConfig{Clusters: testEntries}); err != nil {
		t.Fatalf("WriteClusterConfig: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, objectKey("agentbox-system", "clusters-config"), cm); err != nil {
		t.Fatalf("expected ConfigMap to exist: %v", err)
	}

	yamlData, ok := cm.Data[ClusterConfigKey]
	if !ok || yamlData == "" {
		t.Fatalf("%s key missing or empty", ClusterConfigKey)
	}

	// Verify round-trip: parse back and check IDs.
	store := NewStore()
	if err := store.LoadFromData([]byte(yamlData)); err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if _, ok := store.Get("cluster-a"); !ok {
		t.Error("cluster-a missing after round-trip")
	}
	if e, ok := store.Get("cluster-a"); !ok || e.Gateway == nil {
		t.Error("cluster-a gateway missing after round-trip")
	}
	if _, ok := store.Get("cluster-b"); !ok {
		t.Error("cluster-b missing after round-trip")
	}
}

func TestWriteClusterConfig_UpdatesExistingConfigMap(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "agentbox-system",
			Name:      "clusters-config",
		},
		Data: map[string]string{
			ClusterConfigKey: "clusters: []",
			"other-key":      "preserve-me",
		},
	}
	c := newFakeClient(existing).Build()
	ctx := context.Background()

	if err := WriteClusterConfig(ctx, c, "agentbox-system", "clusters-config", ClusterConfig{Clusters: testEntries}); err != nil {
		t.Fatalf("WriteClusterConfig: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, objectKey("agentbox-system", "clusters-config"), cm); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}

	// The new snapshot should reflect the new entries.
	store := NewStore()
	if err := store.LoadFromData([]byte(cm.Data[ClusterConfigKey])); err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if _, ok := store.Get("cluster-a"); !ok {
		t.Error("cluster-a missing after update")
	}

	// Other keys must be preserved.
	if cm.Data["other-key"] != "preserve-me" {
		t.Errorf("other-key overwritten: %q", cm.Data["other-key"])
	}
}

func TestConfigMapWriter_EmptyEntriesIsNoOp(t *testing.T) {
	c := newFakeClient().Build()
	w := NewConfigMapWriter(c, "agentbox-system", "clusters-config")

	if err := w.ApplyClusterConfig(context.Background(), ClusterConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cm := &corev1.ConfigMap{}
	err := c.Get(context.Background(), objectKey("agentbox-system", "clusters-config"), cm)
	if err == nil {
		t.Fatal("expected ConfigMap to be absent, but it exists")
	}
}

func TestConfigMapWriter_WritesGatewayFields(t *testing.T) {
	c := newFakeClient().Build()
	w := NewConfigMapWriter(c, "agentbox-system", "clusters-config")
	ctx := context.Background()

	if err := w.ApplyClusterConfig(ctx, ClusterConfig{Clusters: testEntries}); err != nil {
		t.Fatalf("ApplyClusterConfig: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, objectKey("agentbox-system", "clusters-config"), cm); err != nil {
		t.Fatalf("get: %v", err)
	}

	store := NewStore()
	if err := store.LoadFromData([]byte(cm.Data[ClusterConfigKey])); err != nil {
		t.Fatalf("parse: %v", err)
	}

	e, ok := store.Get("cluster-a")
	if !ok {
		t.Fatal("cluster-a not found")
	}
	if e.Gateway == nil {
		t.Fatal("gateway field is nil")
	}
	if got := e.Gateway.NativeAPIBaseURL(); got != "https://native.cluster-a.internal" {
		t.Errorf("NativeAPIBaseURL = %q", got)
	}
	if e.Gateway.Headers["X-Token"] != "secret" {
		t.Error("gateway headers not persisted")
	}
}

func TestConfigMapWriter_PersistsHostAliases(t *testing.T) {
	c := newFakeClient().Build()
	w := NewConfigMapWriter(c, "agentbox-system", "clusters-config")
	ctx := context.Background()

	cfg := ClusterConfig{
		Clusters: testEntries,
		HostAliases: []corev1.HostAlias{
			{IP: "10.0.0.1", Hostnames: []string{"loadbalancer-cluster1.example.com"}},
			{IP: "10.0.0.2", Hostnames: []string{"loadbalancer-cluster2.example.com", "loadbalancer-cluster3.example.com"}},
		},
	}
	if err := w.ApplyClusterConfig(ctx, cfg); err != nil {
		t.Fatalf("ApplyClusterConfig: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, objectKey("agentbox-system", "clusters-config"), cm); err != nil {
		t.Fatalf("get: %v", err)
	}

	store := NewStore()
	if err := store.LoadFromData([]byte(cm.Data[ClusterConfigKey])); err != nil {
		t.Fatalf("parse: %v", err)
	}
	aliases := store.HostAliases()
	if len(aliases) != 2 {
		t.Fatalf("expected 2 host aliases, got %d", len(aliases))
	}
	if aliases[1].Hostnames[1] != "loadbalancer-cluster3.example.com" {
		t.Errorf("unexpected alias[1]: %+v", aliases[1])
	}
}

func TestLoadFromConfigMapObject_LegacyKeyFallback(t *testing.T) {
	// Older Managers wrote only the legacy "clusters.yaml" key. A Worker
	// running the new build must still ingest that format during rolling
	// upgrades — verify the fallback path.
	cm := &corev1.ConfigMap{
		Data: map[string]string{
			LegacyClustersKey: "clusters:\n- id: legacy-cluster\n  url: https://legacy.example.com\n",
		},
	}
	store := NewStore()
	if err := store.LoadFromConfigMapObject(cm); err != nil {
		t.Fatalf("LoadFromConfigMapObject: %v", err)
	}
	if _, ok := store.Get("legacy-cluster"); !ok {
		t.Error("legacy-cluster missing from store")
	}
}
