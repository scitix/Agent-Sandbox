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

package domain

import (
	"encoding/json"
	"errors"
	"testing"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

func TestAppError_Basic(t *testing.T) {
	err := NewNotFound("sandbox not found")
	if err.Code != ErrCodeNotFound {
		t.Fatalf("expected ErrCodeNotFound, got %d", err.Code)
	}
	if err.Error() != "sandbox not found" {
		t.Fatalf("unexpected message: %s", err.Error())
	}
	if err.Unwrap() != nil {
		t.Fatal("expected nil cause for NewNotFound")
	}
}

func TestAppError_WithCause(t *testing.T) {
	cause := errors.New("underlying k8s error")
	err := NewInternal("internal error", cause)
	if err.Code != ErrCodeInternal {
		t.Fatalf("expected ErrCodeInternal, got %d", err.Code)
	}
	if !errors.Is(err.Unwrap(), cause) {
		t.Fatal("expected Unwrap to return the cause")
	}
}

func TestAppError_WithDetail(t *testing.T) {
	detail := &PoolStatusDetail{
		Idle:       0,
		Running:    3,
		Stopping:   2,
		Hint:       "pods stopping",
		RetryAfter: 30,
	}
	err := NewConflict("no idle pods available")
	err.Detail = detail

	if err.Code != ErrCodeConflict {
		t.Fatalf("expected ErrCodeConflict, got %d", err.Code)
	}
	if err.Detail == nil {
		t.Fatal("expected non-nil Detail")
	}
	got, ok := err.Detail.(*PoolStatusDetail)
	if !ok {
		t.Fatalf("expected *PoolStatusDetail, got %T", err.Detail)
	}
	if got.Stopping != 2 {
		t.Fatalf("expected Stopping=2, got %d", got.Stopping)
	}
	if got.RetryAfter != 30 {
		t.Fatalf("expected RetryAfter=30, got %d", got.RetryAfter)
	}
}

func TestPoolStatusDetail_Serialization(t *testing.T) {
	detail := PoolStatusDetail{
		Idle:       1,
		Running:    4,
		Starting:   0,
		Stopping:   2,
		Failed:     0,
		Hint:       "2 pod(s) are currently stopping",
		RetryAfter: 30,
	}

	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var recovered PoolStatusDetail
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if recovered.Idle != 1 {
		t.Fatalf("expected Idle=1, got %d", recovered.Idle)
	}
	if recovered.Running != 4 {
		t.Fatalf("expected Running=4, got %d", recovered.Running)
	}
	if recovered.Stopping != 2 {
		t.Fatalf("expected Stopping=2, got %d", recovered.Stopping)
	}
	if recovered.Hint != "2 pod(s) are currently stopping" {
		t.Fatalf("unexpected hint: %s", recovered.Hint)
	}
	if recovered.RetryAfter != 30 {
		t.Fatalf("expected RetryAfter=30, got %d", recovered.RetryAfter)
	}
}

func TestPoolStatusDetail_ZeroValuesOmitted(t *testing.T) {
	detail := PoolStatusDetail{
		Idle:    5,
		Running: 2,
	}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Hint and RetryAfter should be omitted when zero
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map failed: %v", err)
	}
	if _, ok := raw["hint"]; ok {
		t.Fatal("expected 'hint' to be omitted when empty")
	}
	if _, ok := raw["retryAfter"]; ok {
		t.Fatal("expected 'retryAfter' to be omitted when zero")
	}
}

func TestErrorResponse_WithDetail(t *testing.T) {
	detail := &PoolStatusDetail{
		Idle:     0,
		Stopping: 1,
	}
	resp := gen.ErrorResponse{
		Error:  "no idle pods",
		Detail: detail,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map failed: %v", err)
	}
	if _, ok := raw["detail"]; !ok {
		t.Fatal("expected 'detail' field in response when non-nil")
	}
}

func TestErrorResponse_NoDetailOmitted(t *testing.T) {
	resp := gen.ErrorResponse{Error: "not found"}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map failed: %v", err)
	}
	if _, ok := raw["detail"]; ok {
		t.Fatal("expected 'detail' to be omitted when nil")
	}
}
