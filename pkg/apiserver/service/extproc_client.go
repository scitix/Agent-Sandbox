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
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ctrlplanev1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/ctrlplane/v1"
)

// ExtProcClient is the Controller-side view of the ExtProc control-plane RPC.
// Implementations are safe for concurrent use; close via Close when the
// process is shutting down.
//
// One call, in one direction: the gateway reports activity it observed, which
// is the only thing it knows that the control plane cannot read from
// Kubernetes. Routing used to travel the other way — a cache the Controller
// pushed into after each claim — and does not any more, because the gateway
// derives it from the sandbox-id label the claim already writes. That deletion
// is what makes the gateway replicable: a gRPC connection pins to one backend
// Pod, so anything pushed over this channel reaches exactly one replica.
type ExtProcClient interface {
	GetLastActive(ctx context.Context) (map[string]time.Time, error)
	Close() error
}

// NewExtProcClient dials the ExtProc control-plane gRPC server at target and
// configures per-RPC admin-key credentials. target is a gRPC dial string
// (e.g. "agentbox-extproc.agentbox-system.svc:9003"). For backwards
// compatibility with the old HTTP flag value, any leading "http://" or
// "https://" scheme is stripped silently so stale deployment manifests keep
// working. adminKey may be empty in dev mode; the server side is expected to
// match.
func NewExtProcClient(target, adminKey string) (ExtProcClient, error) {
	if target == "" {
		return nil, fmt.Errorf("extproc client: target is required")
	}
	target = normalizeGRPCTarget(target)
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if adminKey != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(&bearerCreds{token: adminKey}))
	}
	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("extproc client: dial %q: %w", target, err)
	}
	return &grpcExtProcClient{
		conn: conn,
		stub: ctrlplanev1.NewControlPlaneServiceClient(conn),
	}, nil
}

// normalizeGRPCTarget strips an HTTP(S) scheme and trailing slash from a gRPC
// dial target so that a legacy HTTP URL (carried over from when this endpoint
// was served via HTTP) works without a deployment-time edit.
func normalizeGRPCTarget(target string) string {
	for _, prefix := range []string{"http://", "https://"} {
		if after, ok := strings.CutPrefix(target, prefix); ok {
			target = after
			break
		}
	}
	return strings.TrimRight(target, "/")
}

type grpcExtProcClient struct {
	conn *grpc.ClientConn
	stub ctrlplanev1.ControlPlaneServiceClient
}

// GetLastActive fetches the per-sandbox last-activity snapshot. Timestamps
// returned by ExtProc are RFC3339 strings; unparseable entries are skipped.
func (c *grpcExtProcClient) GetLastActive(ctx context.Context) (map[string]time.Time, error) {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := c.stub.GetLastActive(callCtx, &ctrlplanev1.GetLastActiveRequest{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(resp.LastActive))
	for id, ts := range resp.LastActive {
		t, parseErr := time.Parse(time.RFC3339, ts)
		if parseErr != nil {
			continue
		}
		out[id] = t
	}
	return out, nil
}

func (c *grpcExtProcClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// bearerCreds attaches an `authorization: Bearer <token>` metadata header on
// every outgoing RPC. RequireTransportSecurity returns false so the creds work
// over plaintext in-cluster connections.
type bearerCreds struct {
	token string
}

func (b *bearerCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b *bearerCreds) RequireTransportSecurity() bool { return false }
