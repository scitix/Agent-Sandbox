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
	"testing"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// The precision lives in `error_code`, not in a new HTTP status.
//
// This surface is a third-party contract: real E2B client code is expected to
// run against it unchanged, so the create answers only the statuses the spec
// declares. What a caller needs beyond that — "the pool is empty, ask again"
// versus "this request is broken, do not" — travels in the field upstream
// defined for exactly that, and named this value in its own description.
func TestCapacityIsNamedInErrorCode(t *testing.T) {
	cases := map[apidomain.ErrorCode]string{
		apidomain.ErrCodeTooManyRequests:    "sandbox_capacity_unavailable",
		apidomain.ErrCodeServiceUnavailable: "sandbox_capacity_unavailable",
		apidomain.ErrCodeGatewayTimeout:     "sandbox_placement_timeout",
		apidomain.ErrCodeInternal:           "sandbox_create_failed",
		// A caller error is already fully described by its status and message.
		apidomain.ErrCodeBadRequest: "",
		apidomain.ErrCodeNotFound:   "",
	}
	for code, want := range cases {
		got := e2bErrorCode(&apidomain.AppError{Code: code})
		if got != want {
			t.Errorf("code %d: got %q, want %q", code, got, want)
		}
	}
}

// Exhaustion and a dead backend are both "try again", which is why they share
// a status — but only one of them is the caller's pool being full, and the
// body has to keep saying so.
func TestExhaustionKeepsItsOwnCodeInTheBody(t *testing.T) {
	appErr := apidomain.NewTooManyRequests("no idle sandboxes available in the pool", nil, nil)
	if int32(appErr.Code) != 429 {
		t.Fatalf("pool exhaustion should stay 429 in the body, got %d", appErr.Code)
	}
	if e2bErrorCode(appErr) != "sandbox_capacity_unavailable" {
		t.Fatalf("got %q", e2bErrorCode(appErr))
	}
}
