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
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	ctrlplanev1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/ctrlplane/v1"
)

// fakeServer captures the last request and the authorization metadata so
// tests can assert on them.
type fakeServer struct {
	ctrlplanev1.UnimplementedControlPlaneServiceServer
	lastPush    *ctrlplanev1.PushRouteRequest
	lastEvict   *ctrlplanev1.EvictRouteRequest
	lastAuthHdr string
	pushErr     error
	lastActive  map[string]string
}

func (f *fakeServer) PushRoute(ctx context.Context, req *ctrlplanev1.PushRouteRequest) (*ctrlplanev1.PushRouteResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("authorization"); len(v) > 0 {
			f.lastAuthHdr = v[0]
		}
	}
	f.lastPush = req
	if f.pushErr != nil {
		return nil, f.pushErr
	}
	return &ctrlplanev1.PushRouteResponse{}, nil
}

func (f *fakeServer) EvictRoute(ctx context.Context, req *ctrlplanev1.EvictRouteRequest) (*ctrlplanev1.EvictRouteResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("authorization"); len(v) > 0 {
			f.lastAuthHdr = v[0]
		}
	}
	f.lastEvict = req
	return &ctrlplanev1.EvictRouteResponse{}, nil
}

func (f *fakeServer) GetLastActive(ctx context.Context, _ *ctrlplanev1.GetLastActiveRequest) (*ctrlplanev1.GetLastActiveResponse, error) {
	return &ctrlplanev1.GetLastActiveResponse{LastActive: f.lastActive}, nil
}

func startFakeExtProc(t *testing.T, srvImpl *fakeServer) (ExtProcClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	ctrlplanev1.RegisterControlPlaneServiceServer(srv, srvImpl)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&bearerCreds{token: "s3cret"}),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cli := &grpcExtProcClient{conn: conn, stub: ctrlplanev1.NewControlPlaneServiceClient(conn)}
	return cli, func() {
		_ = conn.Close()
		srv.Stop()
	}
}

func TestExtProcClient_PushRoute_SendsFields(t *testing.T) {
	fake := &fakeServer{}
	cli, cleanup := startFakeExtProc(t, fake)
	defer cleanup()

	err := cli.PushRoute(context.Background(), RouteInfo{
		SandboxID: "sb1",
		Namespace: "default",
		PodName:   "pod-x",
	})
	if err != nil {
		t.Fatalf("PushRoute: %v", err)
	}
	if fake.lastPush == nil {
		t.Fatal("server did not receive a request")
	}
	if fake.lastPush.SandboxId != "sb1" || fake.lastPush.Namespace != "default" ||
		fake.lastPush.PodName != "pod-x" {
		t.Fatalf("unexpected request: %+v", fake.lastPush)
	}
	if fake.lastAuthHdr != "Bearer s3cret" {
		t.Fatalf("expected 'Bearer s3cret', got %q", fake.lastAuthHdr)
	}
}

func TestExtProcClient_PushRoute_ServerErrorPropagates(t *testing.T) {
	fake := &fakeServer{pushErr: status.Error(codes.InvalidArgument, "bad")}
	cli, cleanup := startFakeExtProc(t, fake)
	defer cleanup()

	err := cli.PushRoute(context.Background(), RouteInfo{
		SandboxID: "sb1", Namespace: "default", PodName: "pod-x",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestExtProcClient_EvictRoute_SendsCorrectField(t *testing.T) {
	fake := &fakeServer{}
	cli, cleanup := startFakeExtProc(t, fake)
	defer cleanup()

	if err := cli.EvictRoute(context.Background(), "sb-bye"); err != nil {
		t.Fatalf("EvictRoute: %v", err)
	}
	if fake.lastEvict == nil || fake.lastEvict.SandboxId != "sb-bye" {
		t.Fatalf("unexpected evict request: %+v", fake.lastEvict)
	}
	if fake.lastAuthHdr != "Bearer s3cret" {
		t.Fatalf("expected 'Bearer s3cret', got %q", fake.lastAuthHdr)
	}
}

func TestExtProcClient_GetLastActive_ParsesTimestamps(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fake := &fakeServer{lastActive: map[string]string{
		"sb1": now.Format(time.RFC3339),
		"sb2": "not-a-date",
	}}
	cli, cleanup := startFakeExtProc(t, fake)
	defer cleanup()

	out, err := cli.GetLastActive(context.Background())
	if err != nil {
		t.Fatalf("GetLastActive: %v", err)
	}
	if got := out["sb1"]; !got.Equal(now) {
		t.Fatalf("sb1 timestamp mismatch: got %v, want %v", got, now)
	}
	if _, ok := out["sb2"]; ok {
		t.Fatal("malformed timestamp should be skipped")
	}
}

func TestNewExtProcClient_EmptyTarget(t *testing.T) {
	_, err := NewExtProcClient("", "key")
	if err == nil {
		t.Fatal("expected error for empty target")
	}
}

func TestNormalizeGRPCTarget(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"svc.example.com:9003", "svc.example.com:9003"},
		{"http://svc.example.com:9003", "svc.example.com:9003"},
		{"https://svc.example.com:9003", "svc.example.com:9003"},
		{"http://svc.example.com:9003/", "svc.example.com:9003"},
		{"dns:///svc.example.com:9003", "dns:///svc.example.com:9003"},
		{":9003", ":9003"},
	}
	for _, c := range cases {
		if got := normalizeGRPCTarget(c.in); got != c.want {
			t.Errorf("normalizeGRPCTarget(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
