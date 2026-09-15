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

package extproc

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"

	ctrlplanev1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/ctrlplane/v1"
)

const bufSize = 1024 * 1024

type grpcHarness struct {
	conn   *grpc.ClientConn
	client ctrlplanev1.ControlPlaneServiceClient
	server *grpc.Server
}

func newGRPCHarness(t *testing.T, adminKey string) *grpcHarness {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	tracker := NewActivityTracker()
	tracker.Touch("sb1")

	var srvOpts []grpc.ServerOption
	if adminKey != "" {
		mgr := apikey.NewAdminKeyManager(adminKey)
		srvOpts = append(srvOpts, grpc.UnaryInterceptor(AdminKeyUnaryInterceptor(mgr)))
	}
	srv := grpc.NewServer(srvOpts...)
	ctrlplanev1.RegisterControlPlaneServiceServer(srv, NewInternalGRPCServer(tracker))

	go func() { _ = srv.Serve(lis) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
	})
	return &grpcHarness{
		conn:   conn,
		client: ctrlplanev1.NewControlPlaneServiceClient(conn),
		server: srv,
	}
}

func authCtx(key string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+key)
}

func TestInternalGRPC_GetLastActive(t *testing.T) {
	h := newGRPCHarness(t, "")
	resp, err := h.client.GetLastActive(context.Background(), &ctrlplanev1.GetLastActiveRequest{})
	if err != nil {
		t.Fatalf("GetLastActive: %v", err)
	}
	if _, ok := resp.LastActive["sb1"]; !ok {
		t.Fatalf("missing sb1 in response: %+v", resp.LastActive)
	}
}

func TestInternalGRPC_AuthRejectsMissingMetadata(t *testing.T) {
	h := newGRPCHarness(t, "s3cret")
	_, err := h.client.GetLastActive(context.Background(), &ctrlplanev1.GetLastActiveRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}

func TestInternalGRPC_AuthRejectsBadKey(t *testing.T) {
	h := newGRPCHarness(t, "s3cret")
	_, err := h.client.GetLastActive(authCtx("wrong"), &ctrlplanev1.GetLastActiveRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}

func TestInternalGRPC_AuthAcceptsBearer(t *testing.T) {
	h := newGRPCHarness(t, "s3cret")
	_, err := h.client.GetLastActive(authCtx("s3cret"), &ctrlplanev1.GetLastActiveRequest{})
	if err != nil {
		t.Fatalf("expected OK, got %v", err)
	}
}

func TestInternalGRPC_AuthAcceptsRawKey(t *testing.T) {
	// Interceptor should also accept the raw key without "Bearer " prefix.
	h := newGRPCHarness(t, "s3cret")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "s3cret")
	_, err := h.client.GetLastActive(ctx, &ctrlplanev1.GetLastActiveRequest{})
	if err != nil {
		t.Fatalf("expected OK, got %v", err)
	}
}
