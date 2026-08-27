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

package handlers

import (
	"context"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// ListVolumes returns the PersistentVolumeClaims the caller may mount into a
// sandbox through SandboxEnv.overrides.volumes.
//
// The namespace comes from the authenticated identity and from nowhere else:
// the operation takes no namespace parameter, so a caller cannot enumerate
// another tenant's storage. That is the whole authorisation story for this
// endpoint — the claim must live in the namespace the sandbox Pod will run in,
// and Kubernetes cannot mount across namespaces.
//
// The result is empty (200 OK, items: []) when the feature is disabled; gate UI
// on `volumes` from /v1/feature-gates rather than on the list being non-empty.
func (s *Server) ListVolumes(ctx context.Context, _ gen.ListVolumesRequestObject) (gen.ListVolumesResponseObject, error) {
	if s.volume == nil {
		return gen.ListVolumes200JSONResponse{Items: []gen.VolumeItem{}, Total: 0}, nil
	}
	auth := authFrom(ctx)
	items, appErr := s.volume.List(ctx, auth.Namespace)
	if appErr != nil {
		return gen.ListVolumes500JSONResponse(errResp(ctx, appErr)), nil
	}
	return gen.ListVolumes200JSONResponse{
		Items: items,
		Total: len(items),
	}, nil
}
