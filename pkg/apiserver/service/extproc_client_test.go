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

// fakeServer captures the authorization metadata and serves the one RPC this
// channel still carries.
type fakeServer struct {
	ctrlplanev1.UnimplementedControlPlaneServiceServer
	lastAuthHdr   string
	lastActive    map[string]string
	lastActiveErr error
}

func (f *fakeServer) GetLastActive(ctx context.Context, _ *ctrlplanev1.GetLastActiveRequest) (*ctrlplanev1.GetLastActiveResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("authorization"); len(v) > 0 {
			f.lastAuthHdr = v[0]
		}
	}
	if f.lastActiveErr != nil {
		return nil, f.lastActiveErr
	}
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

// The admin key has to reach the gateway on every call. This assertion used to
// ride along on PushRoute; with routing gone, GetLastActive is the only RPC
// left to carry it, and an unauthenticated control channel would be a quiet
// regression — the server would refuse, the reconciler would log and skip, and
// idle sandboxes would simply stop being reclaimed.
func TestExtProcClient_SendsTheAdminKey(t *testing.T) {
	fake := &fakeServer{lastActive: map[string]string{}}
	cli, cleanup := startFakeExtProc(t, fake)
	defer cleanup()

	if _, err := cli.GetLastActive(context.Background()); err != nil {
		t.Fatalf("GetLastActive: %v", err)
	}
	if fake.lastAuthHdr != "Bearer s3cret" {
		t.Fatalf("expected 'Bearer s3cret', got %q", fake.lastAuthHdr)
	}
}

// A server-side failure must reach the caller rather than being reported as an
// empty snapshot: the reconciler skips its sweep on error, but an empty map
// would read as "nothing has been active", which releases live sandboxes.
func TestExtProcClient_ServerErrorPropagates(t *testing.T) {
	fake := &fakeServer{lastActiveErr: status.Error(codes.InvalidArgument, "bad")}
	cli, cleanup := startFakeExtProc(t, fake)
	defer cleanup()

	out, err := cli.GetLastActive(context.Background())
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
	if out != nil {
		t.Fatalf("a failed call must not return a snapshot, got %+v", out)
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
