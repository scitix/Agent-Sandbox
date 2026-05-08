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

package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ── helpers ───────────────────────────────────────────────────────────────────

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// syncPair returns two connected *websocket.Conn (server, client).
// The server side is already registered with syncSvc via OnConnect.
// The caller owns both conns and must close them.
func syncPair(t *testing.T, syncSvc service.SyncService) (serverConn, clientConn *websocket.Conn) {
	t.Helper()

	connected := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("server upgrade: %v", err)
			return
		}
		syncSvc.OnConnect(conn)
		connected <- conn
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	serverConn = <-connected
	return serverConn, client
}

func newFakeSyncStore(t *testing.T) *apikey.SecretKeyStore {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	return apikey.NewSecretKeyStore(apikey.SecretKeyStoreConfig{
		Client:           fake.NewClientBuilder().WithScheme(s).Build(),
		SecretsNamespace: "agentbox-system",
		CacheTTL:         time.Minute,
	})
}

// newFakeTemplateService creates a SandboxTemplateService backed by a fake K8s client.
func newFakeTemplateService(t *testing.T, objs ...any) service.SandboxTemplateService {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("get fake client builder: %v", err)
	}
	for _, o := range objs {
		if v, ok := o.(*agentsv1alpha1.SandboxTemplate); ok {
			cb = cb.WithObjects(v)
		}
	}
	return service.NewSandboxTemplateService(cb.Build())
}

// makeSyncTestTemplate creates a minimal SandboxTemplate for testing sync.
func makeSyncTestTemplate(name string) *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     "1.0.0",
			Description: "sync test template " + name,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "busybox:latest"}},
					},
				},
			},
		},
	}
}

// ── HandleIncoming: key_sync ──────────────────────────────────────────────────

func TestHandleIncoming_KeySync_WritesLocalSecret(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	tokenHash := "aaaa1111bbbb2222aaaa1111bbbb2222aaaa1111bbbb2222aaaa1111bbbb2222"
	hashPrefix := tokenHash[:16]

	event := service.SyncEvent{
		Type:       service.FrameKeySync,
		TokenHash:  tokenHash,
		HashPrefix: hashPrefix,
		Namespace:  "test-ns",
		User:       "alice",
		Team:       "eng",
		Role:       "tenant",
		IssuedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := svc.HandleIncoming(ctx, event); err != nil {
		t.Fatalf("HandleIncoming(key_sync) error = %v", err)
	}

	// The Secret should now exist in the local store.
	got, err := ks.Get(ctx, "agentbox-apikey-"+hashPrefix)
	if err != nil {
		t.Fatalf("Get() after key_sync error = %v", err)
	}
	if got.User != "alice" {
		t.Errorf("User = %q, want %q", got.User, "alice")
	}
	if got.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", got.Namespace, "test-ns")
	}
}

func TestHandleIncoming_KeySync_Idempotent(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	tokenHash := "bbbb2222cccc3333bbbb2222cccc3333bbbb2222cccc3333bbbb2222cccc3333"
	hashPrefix := tokenHash[:16]
	event := service.SyncEvent{
		Type: service.FrameKeySync, TokenHash: tokenHash, HashPrefix: hashPrefix,
		Namespace: "ns-idem", User: "bob",
	}

	for i := range 3 {
		if err := svc.HandleIncoming(ctx, event); err != nil {
			t.Fatalf("HandleIncoming call %d error = %v", i+1, err)
		}
	}

	// Only one Secret should exist.
	all, err := ks.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 1 {
		t.Errorf("List() = %d items, want 1", len(all))
	}
}

// ── HandleIncoming: key_delete_sync ──────────────────────────────────────────

func TestHandleIncoming_KeyDeleteSync(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	// First, create a key via key_sync.
	tokenHash := "cccc3333dddd4444cccc3333dddd4444cccc3333dddd4444cccc3333dddd4444"
	hashPrefix := tokenHash[:16]
	_ = svc.HandleIncoming(ctx, service.SyncEvent{
		Type: service.FrameKeySync, TokenHash: tokenHash, HashPrefix: hashPrefix,
		Namespace: "ns-del", User: "carol",
	})

	secretName := "agentbox-apikey-" + hashPrefix

	// Verify it exists.
	if _, err := ks.Get(ctx, secretName); err != nil {
		t.Fatalf("Get() before delete error = %v", err)
	}

	// Now delete via key_delete_sync.
	if err := svc.HandleIncoming(ctx, service.SyncEvent{
		Type: service.FrameKeyDeleteSync,
		Name: secretName,
	}); err != nil {
		t.Fatalf("HandleIncoming(key_delete_sync) error = %v", err)
	}

	// Should be gone.
	if _, err := ks.Get(ctx, secretName); err != apikey.ErrTokenNotFound {
		t.Errorf("Get() after delete = %v, want ErrTokenNotFound", err)
	}
}

func TestHandleIncoming_KeyDeleteSync_NotFoundIsIgnored(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	// Deleting a non-existent key should not return an error.
	err := svc.HandleIncoming(ctx, service.SyncEvent{
		Type: service.FrameKeyDeleteSync,
		Name: "agentbox-apikey-doesnotexist",
	})
	if err != nil {
		t.Errorf("HandleIncoming(key_delete_sync, not found) error = %v, want nil", err)
	}
}

// ── HandleIncoming: key_snapshot ─────────────────────────────────────────────

func TestHandleIncoming_KeySnapshot(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	items := []service.SyncEvent{
		{
			TokenHash:  "eeee5555ffff6666eeee5555ffff6666eeee5555ffff6666eeee5555ffff6666",
			HashPrefix: "eeee5555ffff6666",
			Namespace:  "ns-snap", User: "dan",
		},
		{
			TokenHash:  "ffff6666aaaa7777ffff6666aaaa7777ffff6666aaaa7777ffff6666aaaa7777",
			HashPrefix: "ffff6666aaaa7777",
			Namespace:  "ns-snap", User: "eve",
		},
	}

	if err := svc.HandleIncoming(ctx, service.SyncEvent{
		Type:  service.FrameKeySnapshot,
		Items: items,
	}); err != nil {
		t.Fatalf("HandleIncoming(key_snapshot) error = %v", err)
	}

	all, err := ks.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List() = %d items, want 2", len(all))
	}
}

// ── RequestCreate: not connected ──────────────────────────────────────────────

func TestRequestCreate_NotConnected(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	_, err := svc.RequestCreate(ctx, service.CreateKeyRequest{
		Namespace: "ns", User: "frank",
	})
	if err != service.ErrSyncNotConnected {
		t.Errorf("RequestCreate() = %v, want ErrSyncNotConnected", err)
	}
}

// ── RequestDelete: not connected ─────────────────────────────────────────────

func TestRequestDelete_NotConnected(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)
	ctx := context.Background()

	err := svc.RequestDelete(ctx, "agentbox-apikey-somekey")
	if err != service.ErrSyncNotConnected {
		t.Errorf("RequestDelete() = %v, want ErrSyncNotConnected", err)
	}
}

// ── RequestCreate: success (in-process WS) ────────────────────────────────────

func TestRequestCreate_Success(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)

	serverConn, clientConn := syncPair(t, svc)
	defer clientConn.Close() //nolint:errcheck
	defer serverConn.Close() //nolint:errcheck

	// Start HandleIncoming loop on the *server* side — this routes response
	// frames received from the client (ws-proxy simulator) to pending channels.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		for {
			var event service.SyncEvent
			if err := serverConn.ReadJSON(&event); err != nil {
				return
			}
			_ = svc.HandleIncoming(ctx, event)
		}
	}()

	// "ws-proxy" side: read the key_create frame and write back a success resp.
	go func() {
		var frame map[string]any
		if err := clientConn.ReadJSON(&frame); err != nil {
			return
		}
		id, _ := frame["id"].(string)
		_ = clientConn.WriteJSON(map[string]any{
			"id":         id,
			"type":       "key_create_resp",
			"ok":         true,
			"rawToken":   "agbx_testtoken",
			"keyId":      "agentbox-apikey-abcd1234abcd1234",
			"hashPrefix": "abcd1234abcd1234",
			"issuedAt":   time.Now().UTC().Format(time.RFC3339),
		})
	}()

	resp, err := svc.RequestCreate(ctx, service.CreateKeyRequest{
		Namespace: "test-ns", User: "alice",
	})
	if err != nil {
		t.Fatalf("RequestCreate() error = %v", err)
	}
	if resp.RawToken != "agbx_testtoken" {
		t.Errorf("RawToken = %q, want %q", resp.RawToken, "agbx_testtoken")
	}
	if resp.KeyID != "agentbox-apikey-abcd1234abcd1234" {
		t.Errorf("KeyID = %q, want %q", resp.KeyID, "agentbox-apikey-abcd1234abcd1234")
	}
}

// ── RequestCreate: error response (e.g. quota exceeded) ──────────────────────

func TestRequestCreate_ErrorResponse(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)

	serverConn, clientConn := syncPair(t, svc)
	defer clientConn.Close() //nolint:errcheck
	defer serverConn.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		for {
			var event service.SyncEvent
			if err := serverConn.ReadJSON(&event); err != nil {
				return
			}
			_ = svc.HandleIncoming(ctx, event)
		}
	}()

	go func() {
		var frame map[string]any
		if err := clientConn.ReadJSON(&frame); err != nil {
			return
		}
		id, _ := frame["id"].(string)
		_ = clientConn.WriteJSON(map[string]any{
			"id":         id,
			"type":       "key_create_resp",
			"ok":         false,
			"error":      "exceeded max keys per user",
			"httpStatus": 409,
		})
	}()

	_, err := svc.RequestCreate(ctx, service.CreateKeyRequest{Namespace: "ns", User: "bob"})
	if err == nil {
		t.Fatal("RequestCreate() expected error, got nil")
	}
	var httpErr *service.SyncHTTPError
	if !isHTTPError(err, &httpErr) || httpErr.Status != 409 {
		t.Errorf("RequestCreate() error = %v, want SyncHTTPError{Status:409}", err)
	}
}

// ── RequestDelete: success ────────────────────────────────────────────────────

func TestRequestDelete_Success(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)

	serverConn, clientConn := syncPair(t, svc)
	defer clientConn.Close() //nolint:errcheck
	defer serverConn.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		for {
			var event service.SyncEvent
			if err := serverConn.ReadJSON(&event); err != nil {
				return
			}
			_ = svc.HandleIncoming(ctx, event)
		}
	}()

	go func() {
		var frame map[string]any
		if err := clientConn.ReadJSON(&frame); err != nil {
			return
		}
		id, _ := frame["id"].(string)
		_ = clientConn.WriteJSON(map[string]any{
			"id":   id,
			"type": "key_delete_resp",
			"ok":   true,
		})
	}()

	if err := svc.RequestDelete(ctx, "agentbox-apikey-somekey"); err != nil {
		t.Fatalf("RequestDelete() error = %v", err)
	}
}

// ── OnDisconnect drains pending requests ─────────────────────────────────────

func TestOnDisconnect_DrainsPending(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)

	serverConn, clientConn := syncPair(t, svc)
	defer serverConn.Close() //nolint:errcheck

	// Fire RequestCreate in background — it will block waiting for response.
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := svc.RequestCreate(ctx, service.CreateKeyRequest{Namespace: "ns", User: "frank"})
		errCh <- err
	}()

	// Give the goroutine time to send the frame and register the pending channel.
	time.Sleep(50 * time.Millisecond)

	// Read the frame off the wire (so the writer unblocks) and then disconnect.
	var frame map[string]any
	_ = clientConn.ReadJSON(&frame)
	clientConn.Close()                  //nolint:errcheck
	connID := svc.OnConnect(serverConn) // register fresh so we have a connID
	svc.OnDisconnect(connID)

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error after OnDisconnect, got nil")
		}
		// Should be a 503 SyncHTTPError or ErrSyncNotConnected.
		t.Logf("got expected error: %v", err)
	case <-time.After(3 * time.Second):
		t.Error("RequestCreate did not unblock after OnDisconnect")
	}
}

// ── RequestCreate: outbound frame carries correct fields ──────────────────────

func TestRequestCreate_OutboundFrame(t *testing.T) {
	ks := newFakeSyncStore(t)
	svc := service.NewSyncService(ks)

	serverConn, clientConn := syncPair(t, svc)
	defer clientConn.Close() //nolint:errcheck
	defer serverConn.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// HandleIncoming loop on server so responses are routed.
	go func() {
		for {
			var event service.SyncEvent
			if err := serverConn.ReadJSON(&event); err != nil {
				return
			}
			_ = svc.HandleIncoming(ctx, event)
		}
	}()

	// Capture the outbound frame from the client (ws-proxy simulator) side.
	frameCh := make(chan map[string]json.RawMessage, 1)
	go func() {
		_, raw, err := clientConn.ReadMessage()
		if err != nil {
			return
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		frameCh <- m

		// Send a response so RequestCreate doesn't hang.
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		id, _ := req["id"].(string)
		_ = clientConn.WriteJSON(map[string]any{
			"id": id, "type": "key_create_resp", "ok": true,
			"rawToken": "agbx_x", "keyId": "agentbox-apikey-0000000000000000",
		})
	}()

	req := service.CreateKeyRequest{
		Namespace:   "ns-out",
		User:        "grace",
		Team:        "platform",
		Description: "my key",
	}
	_, _ = svc.RequestCreate(ctx, req)

	select {
	case frame := <-frameCh:
		checkJSONString(t, frame, "type", "key_create")
		checkJSONString(t, frame, "namespace", req.Namespace)
		checkJSONString(t, frame, "user", req.User)
		checkJSONString(t, frame, "team", req.Team)
		checkJSONString(t, frame, "description", req.Description)
		if _, ok := frame["id"]; !ok {
			t.Error("outbound frame missing 'id' field")
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for outbound frame")
	}
}

// ── helpers for error type assertions ────────────────────────────────────────

func isHTTPError(err error, target **service.SyncHTTPError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*service.SyncHTTPError); ok {
		*target = e
		return true
	}
	return false
}

func checkJSONString(t *testing.T, m map[string]json.RawMessage, key, want string) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Errorf("frame missing field %q", key)
		return
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("field %q: unmarshal error %v", key, err)
		return
	}
	if got != want {
		t.Errorf("frame[%q] = %q, want %q", key, got, want)
	}
}

// ── HandleIncoming: template_sync ─────────────────────────────────────────────

func TestHandleIncoming_TemplateSync_CreateOrUpdate(t *testing.T) {
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t)
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)
	ctx := context.Background()

	fullRaw, _ := json.Marshal(makeSyncTestTemplate("tmpl-a"))
	event := service.SyncEvent{
		Type:         service.FrameTemplateSync,
		TemplateFull: fullRaw,
	}
	if err := svc.HandleIncoming(ctx, event); err != nil {
		t.Fatalf("HandleIncoming(template_sync) error = %v", err)
	}

	// The template should exist locally after sync.
	got, appErr := tmplSvc.Get(ctx, "tmpl-a")
	if appErr != nil {
		t.Fatalf("Get() after template_sync error = %v", appErr)
	}
	if got.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", got.Version)
	}
}

func TestHandleIncoming_TemplateSync_Idempotent(t *testing.T) {
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t)
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)
	ctx := context.Background()

	fullRaw, _ := json.Marshal(makeSyncTestTemplate("tmpl-idem"))
	event := service.SyncEvent{
		Type:         service.FrameTemplateSync,
		TemplateFull: fullRaw,
	}

	// Apply twice — should not error.
	for i := range 2 {
		if err := svc.HandleIncoming(ctx, event); err != nil {
			t.Fatalf("HandleIncoming attempt %d error = %v", i+1, err)
		}
	}
}

// ── HandleIncoming: template_delete_sync ─────────────────────────────────────

func TestHandleIncoming_TemplateDeleteSync_DeletesLocal(t *testing.T) {
	existing := makeSyncTestTemplate("tmpl-to-del")
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t, existing)
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)
	ctx := context.Background()

	event := service.SyncEvent{
		Type: service.FrameTemplateDeleteSync,
		Name: "tmpl-to-del",
	}
	if err := svc.HandleIncoming(ctx, event); err != nil {
		t.Fatalf("HandleIncoming(template_delete_sync) error = %v", err)
	}

	_, appErr := tmplSvc.Get(ctx, "tmpl-to-del")
	if appErr == nil {
		t.Fatal("expected not-found after template_delete_sync, got nil error")
	}
}

func TestHandleIncoming_TemplateDeleteSync_NotFound_NoError(t *testing.T) {
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t) // empty store
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)
	ctx := context.Background()

	// Deleting a non-existent template should be a no-op (idempotent).
	event := service.SyncEvent{
		Type: service.FrameTemplateDeleteSync,
		Name: "does-not-exist",
	}
	if err := svc.HandleIncoming(ctx, event); err != nil {
		t.Fatalf("HandleIncoming(template_delete_sync) for missing template error = %v", err)
	}
}

// ── HandleIncoming: template_snapshot ────────────────────────────────────────

func TestHandleIncoming_TemplateSnapshot_AppliesAll(t *testing.T) {
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t)
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)
	ctx := context.Background()

	fullA, _ := json.Marshal(makeSyncTestTemplate("tmpl-a"))
	fullB, _ := json.Marshal(makeSyncTestTemplate("tmpl-b"))

	snapshot := service.SyncEvent{
		Type: service.FrameTemplateSnapshot,
		Items: []service.SyncEvent{
			{Type: service.FrameTemplateSync, TemplateFull: fullA},
			{Type: service.FrameTemplateSync, TemplateFull: fullB},
		},
	}
	if err := svc.HandleIncoming(ctx, snapshot); err != nil {
		t.Fatalf("HandleIncoming(template_snapshot) error = %v", err)
	}

	for _, name := range []string{"tmpl-a", "tmpl-b"} {
		if _, appErr := tmplSvc.Get(ctx, name); appErr != nil {
			t.Errorf("Get(%q) after snapshot error = %v", name, appErr)
		}
	}
}

// ── RequestTemplateCreate / RequestTemplateDelete via WS ─────────────────────

func TestRequestTemplateCreate_ForwardsToWsProxy(t *testing.T) {
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t)
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)

	// Simulate ws-proxy: listen on a WS server, echo a template_create_resp.
	serverConn, clientConn := syncPair(t, svc)
	defer clientConn.Close() //nolint:errcheck

	// Start a HandleIncoming loop so response frames are routed to pending channels.
	go func() {
		for {
			var event service.SyncEvent
			if err := serverConn.ReadJSON(&event); err != nil {
				return
			}
			_ = svc.HandleIncoming(context.Background(), event)
		}
	}()

	// ws-proxy goroutine: read the create request and send back success.
	go func() {
		var frame map[string]json.RawMessage
		if err := clientConn.ReadJSON(&frame); err != nil {
			return
		}
		var id string
		_ = json.Unmarshal(frame["id"], &id)
		resp := map[string]any{
			"id":           id,
			"type":         "template_create_resp",
			"ok":           true,
			"templateName": "tmpl-x",
		}
		_ = clientConn.WriteJSON(resp)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tmplRaw, _ := json.Marshal(makeSyncTestTemplate("tmpl-x"))
	if err := svc.RequestTemplateCreate(ctx, tmplRaw); err != nil {
		t.Fatalf("RequestTemplateCreate error = %v", err)
	}
}

func TestRequestTemplateDelete_ForwardsToWsProxy(t *testing.T) {
	ks := newFakeSyncStore(t)
	tmplSvc := newFakeTemplateService(t)
	svc := service.NewSyncServiceWithTemplate(ks, tmplSvc)

	serverConn, clientConn := syncPair(t, svc)
	defer clientConn.Close() //nolint:errcheck

	// Start a HandleIncoming loop so response frames are routed to pending channels.
	go func() {
		for {
			var event service.SyncEvent
			if err := serverConn.ReadJSON(&event); err != nil {
				return
			}
			_ = svc.HandleIncoming(context.Background(), event)
		}
	}()

	go func() {
		var frame map[string]json.RawMessage
		if err := clientConn.ReadJSON(&frame); err != nil {
			return
		}
		var id string
		_ = json.Unmarshal(frame["id"], &id)
		resp := map[string]any{
			"id":           id,
			"type":         "template_delete_resp",
			"ok":           true,
			"templateName": "tmpl-y",
		}
		_ = clientConn.WriteJSON(resp)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := svc.RequestTemplateDelete(ctx, "tmpl-y"); err != nil {
		t.Fatalf("RequestTemplateDelete error = %v", err)
	}
}

// ── ClusterConfig sync tests ──────────────────────────────────────────────────

// realClusterSink records the received snapshot directly.
type realClusterSink struct {
	applied []cluster.ClusterEntry
	aliases []corev1.HostAlias
	err     error
}

func (r *realClusterSink) ApplyClusterConfig(_ context.Context, cfg cluster.ClusterConfig) error {
	if r.err != nil {
		return r.err
	}
	r.applied = append(r.applied, cfg.Clusters...)
	r.aliases = append(r.aliases, cfg.HostAliases...)
	return nil
}

func TestHandleIncoming_ClusterConfigSync(t *testing.T) {
	sink := &realClusterSink{}
	svc := service.NewSyncServiceFull(newFakeSyncStore(t), nil, sink)

	snapshot := cluster.ClusterConfig{
		Clusters: []cluster.ClusterEntry{
			{ID: "cluster-a", URL: "https://a.example.com"},
			{ID: "cluster-b", URL: "https://b.example.com", Gateway: &cluster.GatewayConfig{
				NativeURL: "https://native.cluster-b.internal",
			}},
		},
		HostAliases: []corev1.HostAlias{
			{IP: "10.0.0.1", Hostnames: []string{"a.example.com"}},
		},
	}
	raw, _ := json.Marshal(snapshot)

	event := service.SyncEvent{
		Type:           service.FrameClusterConfigSync,
		ConfigSnapshot: raw,
	}

	if err := svc.HandleIncoming(context.Background(), event); err != nil {
		t.Fatalf("HandleIncoming: %v", err)
	}

	if len(sink.applied) != 2 {
		t.Fatalf("expected 2 applied entries, got %d", len(sink.applied))
	}
	if sink.applied[0].ID != "cluster-a" {
		t.Errorf("entry[0].ID = %q", sink.applied[0].ID)
	}
	if sink.applied[1].Gateway == nil {
		t.Error("entry[1].Gateway is nil")
	}
	if len(sink.aliases) != 1 || sink.aliases[0].IP != "10.0.0.1" {
		t.Errorf("unexpected host aliases: %+v", sink.aliases)
	}
}

func TestHandleIncoming_ClusterConfigSync_EmptyIsNoOp(t *testing.T) {
	sink := &realClusterSink{}
	svc := service.NewSyncServiceFull(newFakeSyncStore(t), nil, sink)

	event := service.SyncEvent{
		Type:           service.FrameClusterConfigSync,
		ConfigSnapshot: nil,
	}

	if err := svc.HandleIncoming(context.Background(), event); err != nil {
		t.Fatalf("HandleIncoming: %v", err)
	}

	if len(sink.applied) != 0 {
		t.Errorf("expected 0 applied entries (no-op), got %d", len(sink.applied))
	}
}

func TestHandleIncoming_ClusterConfigSnapshot(t *testing.T) {
	sink := &realClusterSink{}
	svc := service.NewSyncServiceFull(newFakeSyncStore(t), nil, sink)

	snapshot := cluster.ClusterConfig{
		Clusters: []cluster.ClusterEntry{{ID: "cluster-a", URL: "https://a.example.com"}},
	}
	raw, _ := json.Marshal(snapshot)

	event := service.SyncEvent{
		Type:           service.FrameClusterConfigSnapshot,
		ConfigSnapshot: raw,
	}

	if err := svc.HandleIncoming(context.Background(), event); err != nil {
		t.Fatalf("HandleIncoming: %v", err)
	}

	if len(sink.applied) != 1 || sink.applied[0].ID != "cluster-a" {
		t.Errorf("unexpected applied: %v", sink.applied)
	}
}

func TestHandleIncoming_ClusterConfigSync_NilSink(t *testing.T) {
	// When no sink is configured, cluster_config_sync should be a no-op (no panic).
	svc := service.NewSyncServiceFull(newFakeSyncStore(t), nil, nil)

	snapshot := cluster.ClusterConfig{Clusters: []cluster.ClusterEntry{{ID: "cluster-a"}}}
	raw, _ := json.Marshal(snapshot)

	event := service.SyncEvent{
		Type:           service.FrameClusterConfigSync,
		ConfigSnapshot: raw,
	}

	if err := svc.HandleIncoming(context.Background(), event); err != nil {
		t.Fatalf("expected no error with nil sink, got: %v", err)
	}
}
