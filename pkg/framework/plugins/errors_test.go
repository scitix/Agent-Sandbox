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

package plugins

import (
	"errors"
	"testing"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

func TestKindFromAppError(t *testing.T) {
	tests := []struct {
		name string
		err  *domain.AppError
		want PluginErrorKind
	}{
		{"nil error → Unknown", nil, PluginErrKindUnknown},
		{
			name: "Detail tagged InsufficientResources wins over Code",
			err:  &domain.AppError{Code: domain.ErrCodeInternal, Detail: PluginErrKindInsufficientResources},
			want: PluginErrKindInsufficientResources,
		},
		{
			name: "503 falls back to InsufficientResources",
			err:  &domain.AppError{Code: domain.ErrCodeServiceUnavailable},
			want: PluginErrKindInsufficientResources,
		},
		{
			name: "429 falls back to InsufficientResources",
			err:  &domain.AppError{Code: domain.ErrCodeTooManyRequests},
			want: PluginErrKindInsufficientResources,
		},
		{
			name: "422 falls back to InvalidSpec",
			err:  &domain.AppError{Code: domain.ErrCodeUnprocessableEntity},
			want: PluginErrKindInvalidSpec,
		},
		{
			name: "400 falls back to InvalidSpec",
			err:  &domain.AppError{Code: domain.ErrCodeBadRequest},
			want: PluginErrKindInvalidSpec,
		},
		{
			name: "500 falls back to Internal",
			err:  &domain.AppError{Code: domain.ErrCodeInternal},
			want: PluginErrKindInternal,
		},
		{
			name: "404 with no tag falls back to Internal",
			err:  &domain.AppError{Code: domain.ErrCodeNotFound},
			want: PluginErrKindInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KindFromAppError(tt.err); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewInsufficientResources(t *testing.T) {
	cause := errors.New("scheduler quota exhausted")
	err := NewInsufficientResources("no capacity", cause)
	if err.Code != domain.ErrCodeServiceUnavailable {
		t.Errorf("Code = %d, want 503", err.Code)
	}
	if err.Message != "no capacity" {
		t.Errorf("Message = %q", err.Message)
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected Unwrap to chain to cause")
	}
	if KindFromAppError(err) != PluginErrKindInsufficientResources {
		t.Errorf("kind round-trip failed")
	}
}

func TestNewInvalidSpec(t *testing.T) {
	err := NewInvalidSpec("missing label", nil)
	if KindFromAppError(err) != PluginErrKindInvalidSpec {
		t.Error("invalid spec round-trip failed")
	}
}

func TestNewInternal(t *testing.T) {
	err := NewInternal("rpc timeout", nil)
	if KindFromAppError(err) != PluginErrKindInternal {
		t.Error("internal round-trip failed")
	}
}
