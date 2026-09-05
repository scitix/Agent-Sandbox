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
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

// managerWithConn returns a SyncManager with one registered cluster, plus that
// cluster's template channel — the thing a worker would be reading.
func managerWithConn(t *testing.T) (*SyncManager, chan *syncv1.TemplateEvent) {
	t.Helper()
	sc := &clusterSyncConn{
		clusterID: "cluster-a",
		tmplCh:    make(chan *syncv1.TemplateEvent, broadcastBuffer),
		done:      make(chan struct{}),
	}
	m := &SyncManager{registry: map[string]*clusterSyncConn{"cluster-a": sc}}
	return m, sc.tmplCh
}

func watchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return s
}

func templateNamed(name, version string) *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:                 version,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{IdleImage: "idle:1"},
		},
	}
}

func recvEvent(t *testing.T, ch <-chan *syncv1.TemplateEvent) *syncv1.TemplateEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a template event")
		return nil
	}
}

// The reason the watch exists: a template that already exists on the hub when
// the watch starts must be published, because a plain watch reports only what
// happens after it is established — and the window while it was down is exactly
// when an edit goes missing.
func TestWatchTemplates_ResyncsExistingOnStart(t *testing.T) {
	m, ch := managerWithConn(t)
	c := fake.NewClientBuilder().WithScheme(watchScheme(t)).
		WithObjects(templateNamed("e2b-envd", "0.2.26")).Build()

	m.resyncTemplates(context.Background(), c)

	ev := recvEvent(t, ch)
	up := ev.GetUpsert()
	if up == nil {
		t.Fatalf("expected an upsert, got %+v", ev.Kind)
	}
	var got agentsv1alpha1.SandboxTemplate
	if err := json.Unmarshal(up.TemplateJson, &got); err != nil {
		t.Fatalf("payload is not a template: %v", err)
	}
	if got.Name != "e2b-envd" || got.Spec.Version != "0.2.26" {
		t.Fatalf("wrong template published: %s@%s", got.Name, got.Spec.Version)
	}
}

func TestHandleTemplateEvent_UpsertAndDelete(t *testing.T) {
	m, ch := managerWithConn(t)

	m.handleTemplateEvent(watch.Event{Type: watch.Modified, Object: templateNamed("kata-envd", "0.0.3")})
	if up := recvEvent(t, ch).GetUpsert(); up == nil {
		t.Fatal("Modified must publish an upsert")
	}

	m.handleTemplateEvent(watch.Event{Type: watch.Deleted, Object: templateNamed("kata-envd", "0.0.3")})
	del := recvEvent(t, ch).GetDelete()
	if del == nil || del.Name != "kata-envd" {
		t.Fatalf("Deleted must publish a delete for the name, got %+v", del)
	}
}

// An Error event carries a Status object rather than a template. Publishing it
// as an upsert would push a garbage payload at every worker; the closed channel
// that follows is what restarts the watch.
func TestHandleTemplateEvent_IgnoresNonTemplateObjects(t *testing.T) {
	m, ch := managerWithConn(t)
	m.handleTemplateEvent(watch.Event{Type: watch.Error, Object: &metav1.Status{Reason: "Expired"}})
	select {
	case ev := <-ch:
		t.Fatalf("nothing should have been published, got %+v", ev.Kind)
	case <-time.After(200 * time.Millisecond):
	}
}

// A nil client must not panic or spin: the hub can be configured without one.
func TestWatchTemplateCRs_NilClientReturns(t *testing.T) {
	m, _ := managerWithConn(t)
	done := make(chan struct{})
	go func() { m.WatchTemplateCRs(context.Background(), nil); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchTemplateCRs should return immediately without a client")
	}
}
