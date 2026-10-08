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
	"strings"

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
)

// The v2 create and connect operations are what the E2B SDKs call from Python
// e2b 2.51 / JS e2b 2.x onward; older SDKs call the v1 paths. Both generations
// are in use at once, so both are served, and v2 is an adapter over v1 rather
// than a second implementation: the request bodies differ only in what AgentBox
// already ignores (`secure`, which governs envd's own access token while
// AgentBox authenticates at the gateway) and in the optionality of connect's
// timeout. One code path means a fix to create or connect cannot reach one SDK
// generation and miss the other.

// PostV2Sandboxes serves POST /v2/sandboxes.
func (s *Server) PostV2Sandboxes(ctx context.Context, req e2bgen.PostV2SandboxesRequestObject) (e2bgen.PostV2SandboxesResponseObject, error) {
	var body *e2bgen.NewSandbox
	if req.Body != nil {
		body = newSandboxFromV2(req.Body)
	}

	var (
		resp e2bgen.PostSandboxesResponseObject
		err  error
	)
	asV1Path(ctx, func() {
		resp, err = s.PostSandboxes(ctx, e2bgen.PostSandboxesRequestObject{Body: body})
	})
	if err != nil || resp == nil {
		return nil, err
	}

	switch r := resp.(type) {
	case e2bgen.PostSandboxes201JSONResponse:
		return e2bgen.PostV2Sandboxes201JSONResponse(r), nil
	case e2bgen.PostSandboxes400JSONResponse:
		return e2bgen.PostV2Sandboxes400JSONResponse(r), nil
	case e2bgen.PostSandboxes401JSONResponse:
		return e2bgen.PostV2Sandboxes401JSONResponse(r), nil
	case e2bgen.PostSandboxes429JSONResponse:
		return e2bgen.PostV2Sandboxes429JSONResponse(r), nil
	case e2bgen.PostSandboxes500JSONResponse:
		return e2bgen.PostV2Sandboxes500JSONResponse(r), nil
	case e2bgen.PostSandboxes503JSONResponse:
		return e2bgen.PostV2Sandboxes503JSONResponse(r), nil
	case e2bgen.PostSandboxes504JSONResponse:
		return e2bgen.PostV2Sandboxes504JSONResponse(r), nil
	default:
		return e2bgen.PostV2Sandboxes500JSONResponse{N500JSONResponse: e2bgen.N500JSONResponse(errRespCode(500, "unexpected create response"))}, nil
	}
}

// PostV2SandboxesSandboxIDConnect serves POST /v2/sandboxes/{sandboxID}/connect.
//
// Upstream defaults an omitted v2 timeout to 300s. Here an omitted timeout
// leaves the deadline alone, as it does on v1: the SDKs always send one, and
// silently shortening a sandbox created with a longer timeout is the worse
// failure for a caller that did not send it.
func (s *Server) PostV2SandboxesSandboxIDConnect(ctx context.Context, req e2bgen.PostV2SandboxesSandboxIDConnectRequestObject) (e2bgen.PostV2SandboxesSandboxIDConnectResponseObject, error) {
	var body *e2bgen.ConnectSandbox
	if req.Body != nil {
		body = &e2bgen.ConnectSandbox{Memory: req.Body.Memory}
		if req.Body.Timeout != nil {
			body.Timeout = *req.Body.Timeout
		}
	}

	var (
		resp e2bgen.PostSandboxesSandboxIDConnectResponseObject
		err  error
	)
	asV1Path(ctx, func() {
		resp, err = s.PostSandboxesSandboxIDConnect(ctx, e2bgen.PostSandboxesSandboxIDConnectRequestObject{
			SandboxID: req.SandboxID,
			Body:      body,
		})
	})
	if err != nil || resp == nil {
		return nil, err
	}

	switch r := resp.(type) {
	case e2bgen.PostSandboxesSandboxIDConnect200JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect200JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect201JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect201JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect400JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect400JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect401JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect401JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect404JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect404JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect409JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect409JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect429JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect429JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect500JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect500JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect503JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect503JSONResponse(r), nil
	case e2bgen.PostSandboxesSandboxIDConnect504JSONResponse:
		return e2bgen.PostV2SandboxesSandboxIDConnect504JSONResponse(r), nil
	default:
		return e2bgen.PostV2SandboxesSandboxIDConnect500JSONResponse{N500JSONResponse: e2bgen.N500JSONResponse(errRespCode(500, "unexpected connect response"))}, nil
	}
}

// newSandboxFromV2 projects a v2 create body onto the v1 shape. NewSandboxV2 is
// NewSandbox without `secure`; every other field is copied, so a field upstream
// adds to both shows up here as a compile-visible omission rather than a
// silently dropped value.
func newSandboxFromV2(v2 *e2bgen.NewSandboxV2) *e2bgen.NewSandbox {
	return &e2bgen.NewSandbox{
		AllowInternetAccess: v2.AllowInternetAccess,
		AutoPause:           v2.AutoPause,
		AutoPauseMemory:     v2.AutoPauseMemory,
		AutoResume:          v2.AutoResume,
		EnvVars:             v2.EnvVars,
		Iam:                 v2.Iam,
		Mcp:                 v2.Mcp,
		Metadata:            v2.Metadata,
		Network:             v2.Network,
		TemplateID:          v2.TemplateID,
		Timeout:             v2.Timeout,
		VolumeMounts:        v2.VolumeMounts,
	}
}

// asV1Path runs fn with the request path rewritten from /v2/sandboxes… to
// /sandboxes…, and restores it afterwards.
//
// The v1 handlers forward cross-cluster requests by replaying the incoming
// path on the target cluster. Replaying the v1 path keeps forwarding working
// while clusters run different releases, since every release serves v1 and
// only newer ones serve v2. The body forwarded is already the v1 shape. The
// path is restored so access logs and metrics recorded after the handler still
// show the route the caller actually hit.
func asV1Path(ctx context.Context, fn func()) {
	gc := httpctx.GinFromCtx(ctx)
	if gc == nil || gc.Request == nil || gc.Request.URL == nil {
		fn()
		return
	}
	u := gc.Request.URL
	origPath, origRaw := u.Path, u.RawPath
	// Index rather than HasPrefix: the API may be mounted under a base path.
	if i := strings.Index(u.Path, "/v2/sandboxes"); i >= 0 {
		u.Path = u.Path[:i] + "/sandboxes" + u.Path[i+len("/v2/sandboxes"):]
		u.RawPath = ""
	}
	defer func() { u.Path, u.RawPath = origPath, origRaw }()
	fn()
}
