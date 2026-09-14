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
	"errors"
	"strings"
	"testing"

	extProcPb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

// statusOf digs the HTTP status out of an ExtProc immediate response.
func statusOf(t *testing.T, resp *extProcPb.ProcessingResponse) int {
	t.Helper()
	imm, ok := resp.Response.(*extProcPb.ProcessingResponse_ImmediateResponse)
	if !ok {
		t.Fatalf("expected an immediate response, got %T", resp.Response)
	}
	return int(imm.ImmediateResponse.Status.Code)
}

func bodyOf(t *testing.T, resp *extProcPb.ProcessingResponse) string {
	t.Helper()
	imm, ok := resp.Response.(*extProcPb.ProcessingResponse_ImmediateResponse)
	if !ok {
		t.Fatalf("expected an immediate response, got %T", resp.Response)
	}
	return string(imm.ImmediateResponse.Body)
}

// The sandbox data plane speaks to E2B clients, and on that surface 502 is how
// a client is told its sandbox is gone: Sandbox.is_running() special-cases it
// to return False — the documented `kill(); is_running() -> False` — while the
// SDK's generic error map turns it into "your sandbox probably timed out".
//
// 404 on the same surface means "no such file in a live sandbox". Serving it
// for a missing sandbox makes is_running raise instead of answering, and tells
// everyone else to go looking for a path that was never the problem.
func TestImmediateError_MissingSandboxIsBadGateway(t *testing.T) {
	resp := immediateError(typev3.StatusCode_BadGateway, "sandbox not found")
	if got := statusOf(t, resp); got != 502 {
		t.Fatalf("a sandbox that is gone must be 502 for the E2B SDK, got %d", got)
	}
	// The two 502s have to stay tellable apart in a log.
	if body := bodyOf(t, resp); !strings.Contains(body, "not found") {
		t.Fatalf("the message must still say which 502 this is, got %q", body)
	}
	unhealthy := immediateError(typev3.StatusCode_BadGateway, ErrSandboxRouteBadGateway.Error())
	if bodyOf(t, unhealthy) == bodyOf(t, resp) {
		t.Fatal("'gone' and 'not healthy yet' must not read identically")
	}
}

// Parameter validation keeps 4xx: the caller's request is malformed, which is
// a different thing from a sandbox that no longer exists, and no SDK path
// reinterprets it.
func TestImmediateError_BadRequestStaysFourHundred(t *testing.T) {
	if got := statusOf(t, immediateError(typev3.StatusCode_BadRequest, "missing sandbox id")); got != 400 {
		t.Fatalf("expected 400 for a malformed request, got %d", got)
	}
}

// The router still distinguishes the two internally — only the wire status is
// shared. Collapsing them in Go would lose the "retry, it may still be coming
// up" vs "stop asking" distinction the cross-cluster and cache paths rely on.
func TestRouteErrors_RemainDistinct(t *testing.T) {
	if errors.Is(ErrSandboxRouteNotFound, ErrSandboxRouteBadGateway) {
		t.Fatal("the two route errors must stay distinguishable in Go")
	}
}
