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
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/scitix/agent-sandbox/pkg/utils/apikey"
)

// AdminKeyUnaryInterceptor returns a grpc.UnaryServerInterceptor that rejects
// any RPC not carrying a valid admin key in the `authorization: Bearer <key>`
// metadata. The caller should only register this interceptor when mgr is
// non-nil; passing nil yields an interceptor that rejects every request.
func AdminKeyUnaryInterceptor(mgr *apikey.AdminKeyManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if mgr == nil {
			return nil, status.Error(codes.Unauthenticated, "admin auth not configured on server")
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		values := md.Get("authorization")
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
		}
		token := strings.TrimSpace(values[0])
		// Accept both "Bearer <key>" (canonical) and a raw key for convenience.
		if after, ok := strings.CutPrefix(token, "Bearer "); ok {
			token = strings.TrimSpace(after)
		}
		if !mgr.IsAdminKey(token) {
			return nil, status.Error(codes.Unauthenticated, "invalid admin key")
		}
		return handler(ctx, req)
	}
}
