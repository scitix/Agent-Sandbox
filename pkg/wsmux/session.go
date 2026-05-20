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

package wsmux

import (
	"context"
	"fmt"
	"net"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// MaxMsgSize bounds gRPC unary / streaming payload size. Set generously so a
// SandboxTemplate snapshot (which can contain hundreds of templates with
// embedded PodTemplateSpec) cannot trip the default 4 MiB cap.
const MaxMsgSize = 64 * 1024 * 1024

// defaultYamuxConfig returns the yamux settings shared by Hub and Worker.
//
// Keepalive is disabled because the surrounding WebSocket already runs its own
// 30 s Ping / 90 s read-deadline keepalive (see syncmgr/manager.go and
// apiserver/handlers/sync.go). Having two layers of keepalive doubles the
// idle traffic for no benefit.
//
// MaxStreamWindowSize is raised to 4 MiB so a single large gRPC frame (e.g. a
// template snapshot) does not block waiting for yamux window updates — gRPC's
// own HTTP/2 flow control already operates inside.
func defaultYamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = false
	cfg.MaxStreamWindowSize = 4 * 1024 * 1024
	return cfg
}

// ServeGRPC wraps wsConn into a yamux server session and starts a gRPC server
// that runs on top of it. The supplied register callback registers all
// service implementations onto the server before Serve() is called.
//
// The Hub side (which dialled the WebSocket) is the yamux server: it Accepts
// streams initiated by the Worker (gRPC client) and feeds them to its
// grpc.Server. ServeGRPC returns once the grpc.Server has begun serving;
// caller blocks on <-session.CloseChan() to detect WebSocket disconnect.
//
// On return, calling grpcSrv.GracefulStop() or session.Close() tears the
// whole stack down; closing wsConn directly is also fine and propagates up.
func ServeGRPC(wsConn *websocket.Conn, register func(*grpc.Server)) (*grpc.Server, *yamux.Session, error) {
	if wsConn == nil {
		return nil, nil, fmt.Errorf("wsmux.ServeGRPC: nil websocket conn")
	}
	netConn := WrapConn(wsConn)
	session, err := yamux.Server(netConn, defaultYamuxConfig())
	if err != nil {
		_ = netConn.Close()
		return nil, nil, fmt.Errorf("yamux.Server: %w", err)
	}

	grpcSrv := grpc.NewServer(
		grpc.MaxRecvMsgSize(MaxMsgSize),
		grpc.MaxSendMsgSize(MaxMsgSize),
	)
	register(grpcSrv)

	go func() {
		// Serve blocks until session.Close. Errors here are expected on
		// graceful shutdown (use of closed network connection); they are
		// logged by callers if they care.
		_ = grpcSrv.Serve(session)
	}()
	return grpcSrv, session, nil
}

// DialGRPC wraps wsConn into a yamux client session and returns a
// *grpc.ClientConn that opens fresh yamux streams on demand. The Worker side
// (which accepted the WebSocket Upgrade) is the yamux client.
//
// The returned ClientConn is lazily connecting (grpc.NewClient semantics): no
// stream is opened until the first RPC. On any RPC, gRPC calls our
// ContextDialer which Opens one yamux stream; gRPC then runs HTTP/2 on that
// stream and multiplexes all subsequent RPCs as HTTP/2 streams within it.
// The outer yamux session is reserved for future non-gRPC streams (e.g.
// terminal byte forwarding) without needing protocol surgery.
func DialGRPC(wsConn *websocket.Conn) (*grpc.ClientConn, *yamux.Session, error) {
	if wsConn == nil {
		return nil, nil, fmt.Errorf("wsmux.DialGRPC: nil websocket conn")
	}
	netConn := WrapConn(wsConn)
	session, err := yamux.Client(netConn, defaultYamuxConfig())
	if err != nil {
		_ = netConn.Close()
		return nil, nil, fmt.Errorf("yamux.Client: %w", err)
	}

	dial := func(_ context.Context, _ string) (net.Conn, error) {
		return session.Open()
	}

	// "passthrough" forces gRPC to use the ContextDialer without name
	// resolution; the authority string is purely cosmetic.
	cc, err := grpc.NewClient("passthrough:///wsmux",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dial),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(MaxMsgSize),
			grpc.MaxCallSendMsgSize(MaxMsgSize),
		),
	)
	if err != nil {
		_ = session.Close()
		return nil, nil, fmt.Errorf("grpc.NewClient: %w", err)
	}
	return cc, session, nil
}
