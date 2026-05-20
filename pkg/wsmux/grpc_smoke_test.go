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

package wsmux_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/scitix/agent-sandbox/pkg/wsmux"
)

// TestServeGRPC_HealthRoundTrip wires up the full yamux+gRPC-over-WebSocket
// stack and exercises an RPC end-to-end. Uses the standard grpc health
// service as a payload-agnostic round-trip target so this smoke test does
// not depend on our own service definitions.
func TestServeGRPC_HealthRoundTrip(t *testing.T) {
	srvWS, cliWS := wsPair(t)

	hub := health.NewServer()
	hub.SetServingStatus("test-service", grpc_health_v1.HealthCheckResponse_SERVING)

	grpcSrv, hubSession, err := wsmux.ServeGRPC(srvWS, func(s *grpc.Server) {
		grpc_health_v1.RegisterHealthServer(s, hub)
	})
	if err != nil {
		t.Fatalf("ServeGRPC: %v", err)
	}
	defer grpcSrv.GracefulStop()
	defer func() { _ = hubSession.Close() }()

	cc, cliSession, err := wsmux.DialGRPC(cliWS)
	if err != nil {
		t.Fatalf("DialGRPC: %v", err)
	}
	defer func() { _ = cc.Close() }()
	defer func() { _ = cliSession.Close() }()

	client := grpc_health_v1.NewHealthClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "test-service"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", resp.Status)
	}

	// Issue many concurrent RPCs to confirm gRPC HTTP/2 streams over a single
	// yamux session don't serialise. If multiplexing were broken these would
	// either deadlock or each pay a full round-trip; we cap them at 1s total.
	start := time.Now()
	const concurrency = 32
	done := make(chan error, concurrency)
	for range concurrency {
		go func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.Check(c, &grpc_health_v1.HealthCheckRequest{Service: "test-service"})
			done <- err
		}()
	}
	for range concurrency {
		if err := <-done; err != nil {
			t.Errorf("concurrent Check: %v", err)
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("32 concurrent Health.Check took %v — multiplexing likely broken", d)
	}
}
