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

// Package extproc implements an Envoy ExternalProcessor (ExtProc) gRPC server.
//
// Architecture:
//
//	Client → Envoy :8092 → ExtProc gRPC (this server)
//	                         ↳ Authenticate API key
//	                         ↳ Extract Sandbox ID (header or URL path)
//	                         ↳ Resolve sandbox Pod IP via Kubernetes
//	                         ↳ Inject "x-envoy-original-dst-host: <PodIP>:<Port>"
//	                         ↳ Rewrite the :path header if needed
//	       → Envoy ORIGINAL_DST cluster → Sandbox Pod FastAPI
package extproc

import (
	"context"
	"errors"
	"fmt"
	"net"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/hostalias"
)

const (
	// apiKeyHeader is the HTTP header name that carries the API key.
	// Envoy normalises all header names to lower-case in HTTP/2 mode.
	apiKeyHeader = "agentbox-api-key"

	// sandboxIDHeader allows callers to pass the sandbox ID explicitly.
	sandboxIDHeader = "x-sandbox-id"

	// sandboxPortHeader allows callers to specify the target port when multiple ports are exposed.
	sandboxPortHeader = "x-sandbox-port"

	// pathSandboxMarker is the URL path segment used for sandbox ID extraction.
	// e.g. /v1/sandboxes/<sandboxId>/<port>/run
	pathSandboxMarker = "sandboxes/"
)

type ServerConfig struct {
	BindAddr   string
	EnableAuth bool
}

// Server is the gRPC ExtProc server that runs alongside the Gin REST API.
type Server struct {
	extProcPb.UnimplementedExternalProcessorServer

	adminKeyMgr     *apikey.AdminKeyManager
	keyStore        apikey.KeyStore
	router          SandboxRouter
	config          ServerConfig
	grpcServer      *grpc.Server
	activityTracker *ActivityTracker // nil = activity tracking disabled
	clusterStore    *cluster.Store   // nil = cross-cluster disabled
	localClusterID  string           // identifier of this cluster
	dnsCache        *dnsCache        // resolves gateway hostnames for ORIGINAL_DST forwarding
}

// New creates a new ExtProc gRPC Server.
//
//   - bindAddr        – TCP address to listen on (e.g. ":9002").
//   - adminKeyMgr     – Admin key manager (pass nil to disable auth / dev mode).
//   - keyStore        – Tenant key store for validating opaque tokens.
//   - router          – Resolves sandbox ID → Pod IP:Port.
//   - activityTracker – Records per-sandbox last-active timestamps (may be nil).
//   - clusterStore    – Cross-cluster config store (nil to disable cross-cluster routing).
//   - localClusterID  – Identifier of the local cluster (empty to disable cross-cluster routing).
func New(cfg ServerConfig, adminKeyMgr *apikey.AdminKeyManager, keyStore apikey.KeyStore, router SandboxRouter, activityTracker *ActivityTracker, clusterStore *cluster.Store, localClusterID string) *Server {
	dns := newDNSCache(defaultDNSTTL)
	// Attach an in-process host-alias resolver driven by the Manager's pushed
	// ClusterConfig. This bypasses Pod-level hostAliases and lets operators
	// add DNS overrides without editing worker-side YAML.
	if clusterStore != nil {
		resolver := hostalias.New()
		resolver.Bind(clusterStore)
		dns.setResolver(resolver)
	}
	return &Server{
		adminKeyMgr:     adminKeyMgr,
		keyStore:        keyStore,
		router:          router,
		config:          cfg,
		activityTracker: activityTracker,
		clusterStore:    clusterStore,
		localClusterID:  localClusterID,
		dnsCache:        dns,
	}
}

// Start listens and serves until ctx is cancelled. It is designed to be run
// in a goroutine alongside the controller-manager and Gin API server.
func (s *Server) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("extproc")

	lis, err := net.Listen("tcp", s.config.BindAddr)
	if err != nil {
		return fmt.Errorf("extproc: listen %s: %w", s.config.BindAddr, err)
	}

	s.grpcServer = grpc.NewServer()
	extProcPb.RegisterExternalProcessorServer(s.grpcServer, s)

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		s.grpcServer.GracefulStop()
	}()

	log.Info("Starting ExtProc gRPC server", "address", s.config.BindAddr)
	if err := s.grpcServer.Serve(lis); err != nil {
		select {
		case <-ctx.Done():
			// Graceful shutdown – not an error.
			<-shutdownDone
			return nil
		default:
			return fmt.Errorf("extproc: gRPC serve: %w", err)
		}
	}
	<-shutdownDone
	return nil
}

// ─── ExternalProcessor implementation ────────────────────────────────────────

// Process implements envoy.service.ext_proc.v3.ExternalProcessor/Process.
//
// The method runs a receive loop over the bidirectional stream. In the current
// configuration Envoy only sends RequestHeaders messages (all body/trailer
// modes are set to NONE/SKIP), so one exchange per HTTP request is expected.
func (s *Server) Process(srv extProcPb.ExternalProcessor_ProcessServer) error {
	ctx := srv.Context()

	for {
		req, err := srv.Recv()
		if err != nil {
			if errors.Is(err, context.Canceled) || grpcStatus.Code(err) == codes.Canceled {
				return nil
			}
			// Stream closed by Envoy after the request is done.
			if grpcStatus.Code(err) == codes.Unavailable {
				return nil
			}
			return err
		}

		switch v := req.Request.(type) {
		case *extProcPb.ProcessingRequest_RequestHeaders:
			resp := s.handleRequestHeaders(ctx, v.RequestHeaders)
			if sendErr := srv.Send(resp); sendErr != nil {
				return sendErr
			}
		default:
			// Should not happen given our processing_mode configuration, but
			// send an empty response to keep the stream alive just in case.
			if sendErr := srv.Send(&extProcPb.ProcessingResponse{}); sendErr != nil {
				return sendErr
			}
		}
	}
}

// handleRequestHeaders is the core logic: auth → sandbox ID extraction →
// route resolution → header mutation (including path rewrite).
func (s *Server) handleRequestHeaders(ctx context.Context, httpHeaders *extProcPb.HttpHeaders) *extProcPb.ProcessingResponse {
	// Build a canonical lower-case header map.
	hdrMap := extractHeaders(httpHeaders)

	// ── 1. Authentication ────────────────────────────────────────────────────
	if s.config.EnableAuth {
		if _, authErr := s.authenticate(ctx, hdrMap); authErr != nil {
			return immediateError(typev3.StatusCode_Unauthorized, "unauthorized")
		}
	}

	// ── 2. Sandbox ID and port extraction ────────────────────────────────────
	target, err := extractTarget(hdrMap)
	if err != nil {
		return immediateError(typev3.StatusCode_BadRequest, err.Error())
	}
	if target.SandboxID == "" {
		return immediateError(typev3.StatusCode_BadRequest,
			"missing sandbox id: set X-Sandbox-ID header or use path /.../sandboxes/{sandboxId}/{port}/...")
	}
	if target.Port == 0 {
		return immediateError(typev3.StatusCode_BadRequest,
			"missing or invalid port: set X-Sandbox-Port header or include port in URL path")
	}

	// ── 2b. Cross-cluster detection ─────────────────────────────────────────
	if s.clusterStore != nil && s.localClusterID != "" {
		cID, rawID := cluster.SplitSandboxID(target.SandboxID)
		if cID != "" && cID != s.localClusterID {
			return s.handleCrossClusterRequest(hdrMap, target, cID)
		}
		// Local cluster prefix → strip it so downstream lookup uses the raw UUID.
		if cID == s.localClusterID {
			target.SandboxID = rawID
		}
	}

	// ── 3. Route resolution ──────────────────────────────────────────────────
	route, routeErr := s.router.ResolveSandboxRoute(ctx, target.SandboxID, target.Port)
	if routeErr != nil {
		if errors.Is(routeErr, ErrSandboxRouteBadGateway) {
			return immediateError(typev3.StatusCode_BadGateway, "sandbox is not in a healthy state")
		}
		if errors.Is(routeErr, ErrSandboxRouteNotFound) {
			return immediateError(typev3.StatusCode_NotFound, "sandbox not found")
		}
		return immediateError(typev3.StatusCode_ServiceUnavailable, "failed to resolve sandbox route")
	}

	// ── 4. Record activity ───────────────────────────────────────────────────
	if s.activityTracker != nil {
		s.activityTracker.Touch(target.SandboxID)
	}

	// ── 5. Inject routing header and rewrite path ────────────────────────────
	dstHost := route.DestHost()
	log := ctrl.Log.WithName("extproc-process").V(1)
	log.Info("Routing request",
		"sandboxId", target.SandboxID, "port", target.Port, "dst", dstHost)

	// Use RawValue ([]byte) instead of Value (string) for all header mutations.
	// Envoy's ORIGINAL_DST load balancer reads x-envoy-original-dst-host via the
	// raw bytes path; passing it as a string Value causes the LB to see an empty
	// override and log "invalid override header value", falling back to the
	// downstream socket address (which is absent in plain HTTP mode) → 503.
	headerMutations := []*corev3.HeaderValueOption{
		{
			Header: &corev3.HeaderValue{
				Key:      "x-envoy-original-dst-host",
				RawValue: []byte(dstHost),
			},
		},
	}

	// Rewrite the :path header if the target includes a rewritten path.
	if target.RewrittenPath != "" && target.RewrittenPath != hdrMap[":path"] {
		headerMutations = append(headerMutations, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:      ":path",
				RawValue: []byte(target.RewrittenPath),
			},
		})
	}

	return &extProcPb.ProcessingResponse{
		Response: &extProcPb.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extProcPb.HeadersResponse{
				Response: &extProcPb.CommonResponse{
					// Do NOT set ClearRouteCache here.
					//
					// Background: Envoy uses "ORIGINAL_DST" cluster which reads the
					// upstream address from the x-envoy-original-dst-host header at
					// connection time – no route re-selection is needed.
					//
					// When ClearRouteCache is true, Envoy discards the already-matched
					// route and re-evaluates the route table with the mutated headers.
					// In practice the re-evaluation runs against a partially-updated
					// header set where the :path pseudo-header mutation has not yet
					// been committed, so Envoy sees an empty URL and fails to find any
					// route, responding with 404 "route_not_found".
					//
					// Keeping ClearRouteCache false means the previously matched static
					// route (prefix "/") is preserved and the ORIGINAL_DST cluster will
					// correctly forward to the PodIP:Port injected in x-envoy-original-dst-host.
					ClearRouteCache: false,
					HeaderMutation: &extProcPb.HeaderMutation{
						SetHeaders: headerMutations,
					},
				},
			},
		},
	}
}
