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

package syncmgr

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

func newEnsureTestManager(t *testing.T, objs ...client.Object) (*SyncManager, client.Client) {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	sm := New(cluster.NewStore(), "sync", "mgr", Deps{
		TemplateClient:         c,
		ImagesCatalogNamespace: "agentbox-system",
		ImagesCatalogConfigMap: "test-images-catalog",
	})
	return sm, c
}

func getCatalogCM(t *testing.T, c client.Client) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{}
	err := c.Get(context.Background(), client.ObjectKey{
		Namespace: "agentbox-system",
		Name:      "test-images-catalog",
	}, cm)
	if err != nil {
		t.Fatalf("get catalog cm: %v", err)
	}
	return cm
}

// When the ConfigMap does not exist, ensure creates it seeded with an empty
// dataset list and the managed-by label.
func TestEnsureImagesCatalog_CreatesEmpty(t *testing.T) {
	sm, c := newEnsureTestManager(t)

	sm.ensureImagesCatalog(context.Background())

	cm := getCatalogCM(t, c)
	if got := cm.Data[imagesCatalogKey]; got != "[]" {
		t.Errorf("seed data = %q, want %q", got, "[]")
	}
	if got := cm.Labels["app.kubernetes.io/managed-by"]; got != "agentbox-dashboard" {
		t.Errorf("managed-by label = %q, want agentbox-dashboard", got)
	}
}

// When the ConfigMap already exists, ensure must NOT overwrite runtime data.
func TestEnsureImagesCatalog_PreservesExisting(t *testing.T) {
	const existing = `[{"id":"d1","name":"Dataset One"}]`
	sm, c := newEnsureTestManager(t, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "agentbox-system",
			Name:      "test-images-catalog",
		},
		Data: map[string]string{imagesCatalogKey: existing},
	})

	sm.ensureImagesCatalog(context.Background())

	cm := getCatalogCM(t, c)
	if got := cm.Data[imagesCatalogKey]; got != existing {
		t.Errorf("ensure overwrote runtime data: got %q, want %q", got, existing)
	}
}

// ensure is a no-op (and does not panic) when no k8s client is configured.
func TestEnsureImagesCatalog_NilClient(t *testing.T) {
	sm := New(cluster.NewStore(), "sync", "mgr", Deps{})
	sm.ensureImagesCatalog(context.Background()) // must not panic
}
