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
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	instancetypeplugin "github.com/scitix/agent-sandbox/pkg/framework/providers/instancetype"
)

// ListInstanceTypes returns the InstanceType catalog. The result is empty
// (200 OK, items: []) when the configured provider is the noop — callers
// should gate UI on `instanceType` from /v1/feature-gates rather than the
// list being non-empty.
func (s *Server) ListInstanceTypes(_ context.Context, _ gen.ListInstanceTypesRequestObject) (gen.ListInstanceTypesResponseObject, error) {
	entries := s.instanceTypeProvider.List()
	items := make([]gen.InstanceTypeItem, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		items = append(items, instanceTypeToGen(e))
	}
	return gen.ListInstanceTypes200JSONResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// instanceTypeToGen flattens an InstanceType into the wire shape. BaseResources
// always renders, even when empty, so clients can branch on map presence
// rather than nil.
func instanceTypeToGen(e *instancetypeplugin.InstanceType) gen.InstanceTypeItem {
	out := gen.InstanceTypeItem{
		Name:          e.Name,
		BaseResources: baseResourcesToGen(e.BaseResources),
	}
	if e.ShowName != "" {
		out.ShowName = ptr.To(e.ShowName)
	}
	if e.Description != "" {
		out.Description = ptr.To(e.Description)
	}
	if e.MaxMultiplier > 0 {
		out.MaxMultiplier = ptr.To(e.MaxMultiplier)
	}
	if e.Cost != "" {
		out.Cost = ptr.To(e.Cost)
	}
	if len(e.Extensions) > 0 {
		ext := make(map[string]string, len(e.Extensions))
		maps.Copy(ext, e.Extensions)
		out.Extensions = &ext
	}
	return out
}

// baseResourcesToGen mirrors the service-layer inlineResourcesToGen helper but
// always returns a value (never nil) — the InstanceType catalog wire shape
// requires `baseResources` to be present even when both maps are empty so the
// dashboard can branch on map keys instead of guarding nil.
func baseResourcesToGen(rr corev1.ResourceRequirements) gen.ResourceRequirements {
	out := gen.ResourceRequirements{}
	if len(rr.Requests) > 0 {
		req := quantityListToMap(rr.Requests)
		out.Requests = &req
	}
	if len(rr.Limits) > 0 {
		lim := quantityListToMap(rr.Limits)
		out.Limits = &lim
	}
	return out
}

func quantityListToMap(rl corev1.ResourceList) map[string]string {
	out := make(map[string]string, len(rl))
	for k, v := range rl {
		out[string(k)] = v.String()
	}
	return out
}
