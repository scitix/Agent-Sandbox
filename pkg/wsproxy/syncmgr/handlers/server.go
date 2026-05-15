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

// Package handlers implements the wsproxygen.StrictServerInterface for the
// internal management API (:9004). It is a thin HTTP adapter layer: each
// method translates a typed request object into a call on the SyncManager and
// returns a typed response object. All business logic and persistence lives in
// the syncmgr package.
package handlers

import (
	"context"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
	"github.com/scitix/agent-sandbox/pkg/wsproxy/syncmgr"
)

// Server implements wsproxygen.StrictServerInterface.
// It holds a reference to the SyncManager and delegates all operations to it.
type Server struct {
	m *syncmgr.SyncManager
}

// New creates a new handlers.Server backed by the given SyncManager.
func New(m *syncmgr.SyncManager) *Server {
	return &Server{m: m}
}

// requireAdmin returns true when the request context contains an admin AuthInfo.
func (s *Server) requireAdmin(ctx context.Context) bool {
	return httpctx.AuthFrom(ctx).Role == apikey.RoleAdmin
}
