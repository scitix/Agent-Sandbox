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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ctrlplanev1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/ctrlplane/v1"
)

// InternalGRPCServer implements the ControlPlaneService gRPC contract for the
// Controller → ExtProc control channel. It wraps the in-memory RouteCache
// (for PushRoute) and the ActivityTracker (for GetLastActive).
type InternalGRPCServer struct {
	ctrlplanev1.UnimplementedControlPlaneServiceServer

	Cache   *RouteCache
	Tracker *ActivityTracker
}

// NewInternalGRPCServer constructs the server. Both dependencies may not be nil.
func NewInternalGRPCServer(cache *RouteCache, tracker *ActivityTracker) *InternalGRPCServer {
	return &InternalGRPCServer{Cache: cache, Tracker: tracker}
}

// PushRoute registers a sandbox_id → (namespace, pod_name) mapping so the
// router can serve traffic without waiting for the informer's sandbox-id
// index to catch up. The mapping carries no phase or IP: those are read live
// from the Pod informer on every request.
func (s *InternalGRPCServer) PushRoute(_ context.Context, req *ctrlplanev1.PushRouteRequest) (*ctrlplanev1.PushRouteResponse, error) {
	if req.GetSandboxId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	if req.GetNamespace() == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}
	if req.GetPodName() == "" {
		return nil, status.Error(codes.InvalidArgument, "pod_name is required")
	}
	if s.Cache == nil {
		return nil, status.Error(codes.FailedPrecondition, "route cache is not configured")
	}
	s.Cache.Put(req.GetSandboxId(), RouteEntry{
		Namespace: req.GetNamespace(),
		PodName:   req.GetPodName(),
	})
	return &ctrlplanev1.PushRouteResponse{}, nil
}

// EvictRoute removes a sandbox_id from the cache. Invoked by the Controller
// when a Pod completes Stopping → Idle, so subsequent router queries for the
// released sandbox_id immediately fall through to the informer fallback path
// (if enabled) or return NotFound, rather than pointing to a stale Pod.
func (s *InternalGRPCServer) EvictRoute(_ context.Context, req *ctrlplanev1.EvictRouteRequest) (*ctrlplanev1.EvictRouteResponse, error) {
	if req.GetSandboxId() == "" {
		return nil, status.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	if s.Cache == nil {
		return nil, status.Error(codes.FailedPrecondition, "route cache is not configured")
	}
	s.Cache.Delete(req.GetSandboxId())
	return &ctrlplanev1.EvictRouteResponse{}, nil
}

// GetLastActive returns per-sandbox activity timestamps as RFC3339 strings.
// Replaces the former HTTP /internal/sandboxes/last-active endpoint.
func (s *InternalGRPCServer) GetLastActive(_ context.Context, _ *ctrlplanev1.GetLastActiveRequest) (*ctrlplanev1.GetLastActiveResponse, error) {
	if s.Tracker == nil {
		return nil, status.Error(codes.FailedPrecondition, "activity tracker is not configured")
	}
	snap := s.Tracker.snapshot()
	out := make(map[string]string, len(snap))
	for id, ts := range snap {
		out[id] = ts.UTC().Format(time.RFC3339)
	}
	return &ctrlplanev1.GetLastActiveResponse{LastActive: out}, nil
}
