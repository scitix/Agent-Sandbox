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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
	"github.com/scitix/agent-sandbox/pkg/wsmux"
)

// testUserAlice is reused across tests that exercise key sync; centralising
// it keeps goconst happy without sprinkling a literal across the file.
const testUserAlice = "alice"

// ── Test harness ──────────────────────────────────────────────────────────────

// fakeHub implements the three sync gRPC services. Each instance lives for
// one test; behaviour is driven through public fields. The Watch* RPCs
// publish events read from per-stream channels so tests can drive snapshots
// and incremental events deterministically.
type fakeHub struct {
	syncv1.UnimplementedAPIKeyServiceServer
	syncv1.UnimplementedTemplateServiceServer
	syncv1.UnimplementedClusterConfigServiceServer

	// Unary RPCs: hook lets a test return canned responses / errors.
	createKeyHook func(ctx context.Context, req *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error)
	deleteKeyHook func(ctx context.Context, req *syncv1.DeleteKeyRequest) (*syncv1.DeleteKeyResponse, error)
	createTplHook func(ctx context.Context, req *syncv1.CreateTemplateRequest) (*syncv1.CreateTemplateResponse, error)
	updateTplHook func(ctx context.Context, req *syncv1.UpdateTemplateRequest) (*syncv1.UpdateTemplateResponse, error)
	deleteTplHook func(ctx context.Context, req *syncv1.DeleteTemplateRequest) (*syncv1.DeleteTemplateResponse, error)

	// Streams: channels are buffered so initial snapshot + several events can
	// be queued before the Worker subscribes.
	keyEvents  chan *syncv1.KeyEvent
	tmplEvents chan *syncv1.TemplateEvent
	cfgEvents  chan *syncv1.ClusterConfigEvent
}

func newFakeHub() *fakeHub {
	return &fakeHub{
		keyEvents:  make(chan *syncv1.KeyEvent, 64),
		tmplEvents: make(chan *syncv1.TemplateEvent, 64),
		cfgEvents:  make(chan *syncv1.ClusterConfigEvent, 64),
	}
}

func (h *fakeHub) CreateKey(ctx context.Context, req *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error) {
	if h.createKeyHook != nil {
		return h.createKeyHook(ctx, req)
	}
	return &syncv1.CreateKeyResponse{}, nil
}
func (h *fakeHub) DeleteKey(ctx context.Context, req *syncv1.DeleteKeyRequest) (*syncv1.DeleteKeyResponse, error) {
	if h.deleteKeyHook != nil {
		return h.deleteKeyHook(ctx, req)
	}
	return &syncv1.DeleteKeyResponse{}, nil
}
func (h *fakeHub) WatchKeys(_ *syncv1.WatchKeysRequest, stream syncv1.APIKeyService_WatchKeysServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-h.keyEvents:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

func (h *fakeHub) CreateTemplate(ctx context.Context, req *syncv1.CreateTemplateRequest) (*syncv1.CreateTemplateResponse, error) {
	if h.createTplHook != nil {
		return h.createTplHook(ctx, req)
	}
	return &syncv1.CreateTemplateResponse{}, nil
}
func (h *fakeHub) UpdateTemplate(ctx context.Context, req *syncv1.UpdateTemplateRequest) (*syncv1.UpdateTemplateResponse, error) {
	if h.updateTplHook != nil {
		return h.updateTplHook(ctx, req)
	}
	return &syncv1.UpdateTemplateResponse{}, nil
}
func (h *fakeHub) DeleteTemplate(ctx context.Context, req *syncv1.DeleteTemplateRequest) (*syncv1.DeleteTemplateResponse, error) {
	if h.deleteTplHook != nil {
		return h.deleteTplHook(ctx, req)
	}
	return &syncv1.DeleteTemplateResponse{}, nil
}
func (h *fakeHub) WatchTemplates(_ *syncv1.WatchTemplatesRequest, stream syncv1.TemplateService_WatchTemplatesServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-h.tmplEvents:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

func (h *fakeHub) WatchClusterConfig(_ *syncv1.WatchClusterConfigRequest, stream syncv1.ClusterConfigService_WatchClusterConfigServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-h.cfgEvents:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// hubSession is the full Hub-side test rig: a httptest server that runs a
// fakeHub gRPC server on top of wsmux. Spawn one per test and call Close to
// tear down both the WebSocket transport and the goroutines.
type hubSession struct {
	srv     *httptest.Server
	wsConn  *websocket.Conn // server-accepted WS
	grpcSrv *grpc.Server    // serving the fake hub
	hub     *fakeHub
}

func startHubAndConnect(t *testing.T) (*hubSession, *grpc.ClientConn) {
	t.Helper()
	hub := newFakeHub()

	srvCh := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		srvCh <- c
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	cliWS, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var srvWS *websocket.Conn
	select {
	case srvWS = <-srvCh:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade timed out")
	}

	grpcSrv, hubSess, err := wsmux.ServeGRPC(srvWS, func(s *grpc.Server) {
		syncv1.RegisterAPIKeyServiceServer(s, hub)
		syncv1.RegisterTemplateServiceServer(s, hub)
		syncv1.RegisterClusterConfigServiceServer(s, hub)
	})
	if err != nil {
		t.Fatalf("ServeGRPC: %v", err)
	}
	_ = hubSess
	t.Cleanup(func() {
		grpcSrv.GracefulStop()
		_ = srvWS.Close()
	})

	cc, cliSess, err := wsmux.DialGRPC(cliWS)
	if err != nil {
		t.Fatalf("DialGRPC: %v", err)
	}
	t.Cleanup(func() {
		_ = cc.Close()
		_ = cliSess.Close()
		_ = cliWS.Close()
	})

	return &hubSession{srv: srv, wsConn: srvWS, grpcSrv: grpcSrv, hub: hub}, cc
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

func makeSyncTestTemplate(name string) *agentsv1alpha1.SandboxTemplate {
	return &agentsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: agentsv1alpha1.SandboxTemplateSpec{
			Version:     "1.0.0",
			Description: "sync test template " + name,
			EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
				IdleImage: "pause:3.10",
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "sandbox", Image: "busybox:latest"}},
					},
				},
			},
		},
	}
}

// stubClusterSink records the latest applied ClusterConfig for assertions.
type stubClusterSink struct {
	mu      sync.Mutex
	applied []cluster.ClusterConfig
}

func (s *stubClusterSink) ApplyClusterConfig(_ context.Context, cfg cluster.ClusterConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, cfg)
	return nil
}
func (s *stubClusterSink) snapshot() []cluster.ClusterConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]cluster.ClusterConfig, len(s.applied))
	copy(out, s.applied)
	return out
}

// ── unary RPC tests ───────────────────────────────────────────────────────────

func TestRequestCreate_NotConnected(t *testing.T) {
	svc := service.NewSyncService(newFakeSyncStore(t))
	_, err := svc.RequestCreate(context.Background(), service.CreateKeyRequest{Namespace: "ns", User: "u"})
	if err != service.ErrSyncNotConnected {
		t.Errorf("err = %v, want ErrSyncNotConnected", err)
	}
}

func TestRequestDelete_NotConnected(t *testing.T) {
	svc := service.NewSyncService(newFakeSyncStore(t))
	if err := svc.RequestDelete(context.Background(), "agentbox-apikey-x"); err != service.ErrSyncNotConnected {
		t.Errorf("err = %v, want ErrSyncNotConnected", err)
	}
}

func TestRequestCreate_Success(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	hub.hub.createKeyHook = func(_ context.Context, req *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error) {
		if req.Namespace != "test-ns" || req.User != testUserAlice {
			t.Errorf("unexpected request: %+v", req)
		}
		return &syncv1.CreateKeyResponse{
			RawToken:   "agbx_testtoken",
			KeyId:      "agentbox-apikey-abcd1234abcd1234",
			HashPrefix: "abcd1234abcd1234",
		}, nil
	}

	svc := service.NewSyncService(newFakeSyncStore(t))
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := svc.RequestCreate(ctx, service.CreateKeyRequest{Namespace: "test-ns", User: testUserAlice})
	if err != nil {
		t.Fatalf("RequestCreate: %v", err)
	}
	if resp.RawToken != "agbx_testtoken" {
		t.Errorf("RawToken = %q, want agbx_testtoken", resp.RawToken)
	}
	if resp.KeyID != "agentbox-apikey-abcd1234abcd1234" {
		t.Errorf("KeyID = %q", resp.KeyID)
	}
}

func TestRequestCreate_ErrorTranslation(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	hub.hub.createKeyHook = func(_ context.Context, _ *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error) {
		return nil, status.Error(codes.AlreadyExists, "exceeded max keys per user")
	}

	svc := service.NewSyncService(newFakeSyncStore(t))
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := svc.RequestCreate(ctx, service.CreateKeyRequest{Namespace: "ns", User: "bob"})
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *service.SyncHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *SyncHTTPError", err)
	}
	if httpErr.Status != 409 {
		t.Errorf("Status = %d, want 409", httpErr.Status)
	}
}

func TestRequestDelete_NotFoundIsTranslated(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	hub.hub.deleteKeyHook = func(_ context.Context, _ *syncv1.DeleteKeyRequest) (*syncv1.DeleteKeyResponse, error) {
		return nil, status.Error(codes.NotFound, "api key not found")
	}

	svc := service.NewSyncService(newFakeSyncStore(t))
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := svc.RequestDelete(ctx, "agentbox-apikey-x")
	var httpErr *service.SyncHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 404 {
		t.Fatalf("err = %v, want SyncHTTPError 404", err)
	}
}

func TestRequestTemplate_ForwardsToHub(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	var calledKind atomic.Value
	hub.hub.createTplHook = func(_ context.Context, req *syncv1.CreateTemplateRequest) (*syncv1.CreateTemplateResponse, error) {
		calledKind.Store("create")
		if len(req.TemplateJson) == 0 {
			t.Error("template_json empty")
		}
		return &syncv1.CreateTemplateResponse{Name: "t"}, nil
	}
	hub.hub.deleteTplHook = func(_ context.Context, req *syncv1.DeleteTemplateRequest) (*syncv1.DeleteTemplateResponse, error) {
		calledKind.Store("delete")
		if req.Name != "demo" {
			t.Errorf("name = %q", req.Name)
		}
		return &syncv1.DeleteTemplateResponse{Name: "demo"}, nil
	}

	svc := service.NewSyncService(newFakeSyncStore(t))
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, _ := json.Marshal(makeSyncTestTemplate("t"))
	if err := svc.RequestTemplateCreate(ctx, raw); err != nil {
		t.Fatalf("create: %v", err)
	}
	if calledKind.Load() != "create" {
		t.Error("create hook not invoked")
	}
	if err := svc.RequestTemplateDelete(ctx, "demo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if calledKind.Load() != "delete" {
		t.Error("delete hook not invoked")
	}
}

// ── Watch tests: stream events drive local state ─────────────────────────────

func TestWatchKeys_AppliesSnapshotAndUpsert(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	ks := newFakeSyncStore(t)

	// Pre-queue a Snapshot and an Upsert so the Worker receives both on subscribe.
	tokenHash := "aaaa1111bbbb2222aaaa1111bbbb2222aaaa1111bbbb2222aaaa1111bbbb2222"
	hub.hub.keyEvents <- &syncv1.KeyEvent{Kind: &syncv1.KeyEvent_Snapshot{Snapshot: &syncv1.KeySnapshot{
		Items: []*syncv1.APIKeyMetadata{{
			TokenHash:  tokenHash,
			HashPrefix: tokenHash[:16],
			Namespace:  "test-ns",
			User:       testUserAlice,
		}},
	}}}

	svc := service.NewSyncService(ks)
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	// Poll until the Secret appears (the Watch goroutine applies asynchronously).
	deadline := time.Now().Add(3 * time.Second)
	var got *apikey.KeyMetadata
	var err error
	for time.Now().Before(deadline) {
		got, err = ks.Get(context.Background(), "agentbox-apikey-"+tokenHash[:16])
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Get after snapshot: %v", err)
	}
	if got.User != testUserAlice {
		t.Errorf("User = %q", got.User)
	}

	// Now push a Delete and assert the Secret is gone.
	hub.hub.keyEvents <- &syncv1.KeyEvent{Kind: &syncv1.KeyEvent_Delete{
		Delete: &syncv1.KeyDelete{SecretName: "agentbox-apikey-" + tokenHash[:16]},
	}}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := ks.Get(context.Background(), "agentbox-apikey-"+tokenHash[:16]); errors.Is(err, apikey.ErrTokenNotFound) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("secret still present after delete event")
}

func TestWatchTemplates_AppliesSnapshotAndDelete(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	tmplSvc := newFakeTemplateService(t)

	t1Raw, _ := json.Marshal(makeSyncTestTemplate("tmpl-a"))
	t2Raw, _ := json.Marshal(makeSyncTestTemplate("tmpl-b"))
	hub.hub.tmplEvents <- &syncv1.TemplateEvent{Kind: &syncv1.TemplateEvent_Snapshot{
		Snapshot: &syncv1.TemplateSnapshot{TemplateJsons: [][]byte{t1Raw, t2Raw}},
	}}

	svc := service.NewSyncServiceWithTemplate(newFakeSyncStore(t), tmplSvc)
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, _ := tmplSvc.List(context.Background(), domain.AuthInfo{}, true)
		if len(items) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	items, _ := tmplSvc.List(context.Background(), domain.AuthInfo{}, true)
	if len(items) != 2 {
		t.Fatalf("snapshot apply: have %d templates, want 2", len(items))
	}

	// Now delete one via stream.
	hub.hub.tmplEvents <- &syncv1.TemplateEvent{Kind: &syncv1.TemplateEvent_Delete{
		Delete: &syncv1.TemplateDelete{Name: "tmpl-a"},
	}}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, _ := tmplSvc.List(context.Background(), domain.AuthInfo{}, true)
		if len(items) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	items, _ = tmplSvc.List(context.Background(), domain.AuthInfo{}, true)
	if len(items) != 1 {
		t.Errorf("after delete: have %d templates, want 1", len(items))
	}
}

func TestWatchClusterConfig_AppliesSnapshot(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	sink := &stubClusterSink{}

	hub.hub.cfgEvents <- &syncv1.ClusterConfigEvent{Snapshot: &syncv1.ClusterConfig{
		Clusters: []*syncv1.ClusterEntry{
			{Id: "cluster-a", Name: "Cluster A", Url: "https://a"},
			{Id: "cluster-b", Name: "Cluster B", Url: "https://b"},
		},
	}}

	svc := service.NewSyncServiceFull(newFakeSyncStore(t), nil, sink)
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.snapshot()) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := sink.snapshot()
	if len(snap) != 1 || len(snap[0].Clusters) != 2 {
		t.Fatalf("sink got %d snapshots; first has %d clusters", len(snap), func() int {
			if len(snap) == 0 {
				return 0
			}
			return len(snap[0].Clusters)
		}())
	}
	if snap[0].Clusters[0].ID != "cluster-a" || snap[0].Clusters[1].ID != "cluster-b" {
		t.Errorf("cluster IDs = %+v", snap[0].Clusters)
	}
}

func TestWatchClusterConfig_EmptySnapshotIsSkipped(t *testing.T) {
	hub, cc := startHubAndConnect(t)
	sink := &stubClusterSink{}

	// Push an empty snapshot followed by a real one. Only the real one should
	// hit the sink — emptiness must never erase existing state.
	hub.hub.cfgEvents <- &syncv1.ClusterConfigEvent{Snapshot: &syncv1.ClusterConfig{}}
	hub.hub.cfgEvents <- &syncv1.ClusterConfigEvent{Snapshot: &syncv1.ClusterConfig{
		Clusters: []*syncv1.ClusterEntry{{Id: "cluster-x", Url: "https://x"}},
	}}

	svc := service.NewSyncServiceFull(newFakeSyncStore(t), nil, sink)
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.snapshot()) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := sink.snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d snapshots, want 1 (empty must be skipped)", len(snap))
	}
}

// ── Multiplexing consistency: big snapshot must not block unary RPCs ─────────

// TestMultiplexing_SnapshotDoesNotBlockRPC is the headline test for this
// migration: a large WatchTemplates snapshot must not stall concurrent
// CreateKey unary RPCs. If yamux/gRPC multiplexing were broken (or the
// adapter were buffering whole WS messages instead of streaming them via
// NextReader) we would see RPC latency spike to seconds. The threshold here
// is generous — failure means multiplexing is fundamentally broken, not
// "just slow".
func TestMultiplexing_SnapshotDoesNotBlockRPC(t *testing.T) {
	hub, cc := startHubAndConnect(t)

	// Wire a permissive hook so CreateKey returns immediately.
	hub.hub.createKeyHook = func(_ context.Context, _ *syncv1.CreateKeyRequest) (*syncv1.CreateKeyResponse, error) {
		return &syncv1.CreateKeyResponse{KeyId: "agentbox-apikey-fake"}, nil
	}

	svc := service.NewSyncService(newFakeSyncStore(t))
	id := svc.OnConnect(cc)
	defer svc.OnDisconnect(id)

	// Queue a large snapshot: 50 templates of ~100KB each (~5 MiB total).
	bigDesc := strings.Repeat("x", 100*1024)
	jsons := make([][]byte, 50)
	for i := range jsons {
		tmpl := makeSyncTestTemplate("t")
		tmpl.Spec.Description = bigDesc
		jsons[i], _ = json.Marshal(tmpl)
	}
	hub.hub.tmplEvents <- &syncv1.TemplateEvent{Kind: &syncv1.TemplateEvent_Snapshot{
		Snapshot: &syncv1.TemplateSnapshot{TemplateJsons: jsons},
	}}

	// Concurrently fire many CreateKey RPCs and measure each one's latency.
	const concurrency = 32
	type result struct {
		latency time.Duration
		err     error
	}
	results := make(chan result, concurrency)
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			t0 := time.Now()
			_, err := svc.RequestCreate(ctx, service.CreateKeyRequest{Namespace: "ns", User: "u"})
			results <- result{latency: time.Since(t0), err: err}
		}()
	}
	wg.Wait()
	close(results)
	totalElapsed := time.Since(start)

	var maxLatency time.Duration
	for r := range results {
		if r.err != nil {
			t.Errorf("RequestCreate error: %v", r.err)
		}
		if r.latency > maxLatency {
			maxLatency = r.latency
		}
	}
	t.Logf("concurrency=%d totalElapsed=%v maxLatency=%v", concurrency, totalElapsed, maxLatency)

	// If multiplexing works the RPC fanout completes in well under 1 s
	// — the WS pair is in-process, snapshot bytes flow on a separate gRPC
	// stream than the unary RPCs. A 2 s ceiling leaves room for slow CI hosts.
	if maxLatency > 2*time.Second {
		t.Errorf("maxLatency %v > 2s — multiplexing likely broken (large snapshot blocking RPCs)", maxLatency)
	}
}
