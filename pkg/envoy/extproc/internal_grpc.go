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
// Controller → ExtProc control channel, which now carries exactly one call.
//
// Routing used to travel here too, as a cache the Controller pushed into. It
// does not any more: the router resolves a sandbox from the Pod informer's
// sandbox-id index, which is populated by the claim itself, so the gateway
// needs to be told nothing to route correctly. What is left is the reverse
// direction — activity observed at the gateway, which only the gateway knows.
type InternalGRPCServer struct {
	ctrlplanev1.UnimplementedControlPlaneServiceServer

	Tracker *ActivityTracker
}

// NewInternalGRPCServer constructs the server.
func NewInternalGRPCServer(tracker *ActivityTracker) *InternalGRPCServer {
	return &InternalGRPCServer{Tracker: tracker}
}

// GetLastActive returns per-sandbox activity timestamps as RFC3339 strings.
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
