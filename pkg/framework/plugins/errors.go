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

import "github.com/scitix/agent-sandbox/pkg/apiserver/domain"

// PluginErrorKind classifies the reason a plugin admission (PreUpdatePool,
// PreCreatePool, ...) failed. The SandboxEnv autoscaler uses it to decide
// whether to binary-search for a smaller acceptable target (cluster ran out
// of resources, but some smaller replicas count might fit) or back off
// entirely (the failure is transient infrastructure or invalid spec).
//
// Plugins should populate this via NewInsufficientResources / NewInvalidSpec
// / NewInternal so callers can switch on the kind without parsing the
// AppError message. Plugins that do not classify themselves are mapped to a
// best-effort kind by KindFromAppError based on the HTTP-mapped error code.
type PluginErrorKind string

const (
	// PluginErrKindUnknown is the default for plugin errors that did not
	// explicitly classify themselves. Treated by callers as Internal —
	// retry later, do not binary-search.
	PluginErrKindUnknown PluginErrorKind = ""

	// PluginErrKindInsufficientResources means the cluster, scheduler, or
	// quota system cannot satisfy the requested replica count, but a
	// smaller count might be admittable. The autoscaler reacts by
	// binary-searching the [current, candidate] range for the largest
	// admit-able value.
	PluginErrKindInsufficientResources PluginErrorKind = "InsufficientResources"

	// PluginErrKindInternal is a transient infrastructure failure (RPC
	// timeout, scheduler unavailable, etc.). The autoscaler does NOT
	// binary-search; it backs off and retries on the next reconcile cycle.
	PluginErrKindInternal PluginErrorKind = "Internal"

	// PluginErrKindInvalidSpec means the pool spec itself is wrong (bad
	// labels, missing fields, validation failure). Retrying with a smaller
	// replicas count would still fail; the autoscaler parks the pool in
	// Saturated/Misconfigured until the spec changes.
	PluginErrKindInvalidSpec PluginErrorKind = "InvalidSpec"
)

// NewInsufficientResources is the canonical helper for plugins to signal
// "scheduler / quota / cluster is full". The returned *AppError carries the
// kind in its Detail so callers can extract it via KindFromAppError without
// parsing the Message string.
//
// cause may be nil; when non-nil it is preserved for logs but not exposed
// to API clients.
func NewInsufficientResources(msg string, cause error) *domain.AppError {
	return &domain.AppError{
		Code:    domain.ErrCodeServiceUnavailable,
		Message: msg,
		Cause:   cause,
		Detail:  PluginErrKindInsufficientResources,
	}
}

// NewInvalidSpec marks a plugin admission failure that won't be fixed by
// retrying with a smaller target (e.g. label validation, missing required
// field).
func NewInvalidSpec(msg string, cause error) *domain.AppError {
	return &domain.AppError{
		Code:    domain.ErrCodeUnprocessableEntity,
		Message: msg,
		Cause:   cause,
		Detail:  PluginErrKindInvalidSpec,
	}
}

// NewInternal marks a transient infrastructure failure. Equivalent to
// domain.NewInternal but also tags PluginErrKindInternal in Detail for
// explicit downstream classification.
func NewInternal(msg string, cause error) *domain.AppError {
	return &domain.AppError{
		Code:    domain.ErrCodeInternal,
		Message: msg,
		Cause:   cause,
		Detail:  PluginErrKindInternal,
	}
}

// KindedDetail is implemented by structured error details that classify
// themselves. A plugin that needs AppError.Detail for its own payload (a
// scheduler response, a quota breakdown) cannot also store a bare
// PluginErrorKind there; implementing this interface on the payload keeps the
// classification explicit instead of leaving it to the HTTP-code fallback.
type KindedDetail interface {
	Kind() PluginErrorKind
}

// KindFromAppError extracts the PluginErrorKind from an *AppError.
//
// Lookup order:
//  1. err.Detail is a PluginErrorKind — return it directly (plugin opted in).
//  2. err.Detail implements KindedDetail — ask it (structured payload that
//     also classifies itself).
//  3. Fall back to mapping err.Code:
//     - 503 ServiceUnavailable / 429 TooManyRequests → InsufficientResources
//     - 400 BadRequest / 422 UnprocessableEntity     → InvalidSpec
//     - anything else (including 500 Internal)       → Internal
//
// The fallback exists so existing plugins that don't yet use the typed
// constructors still produce reasonable classification — closed-source
// schedulers using domain.NewServiceUnavailable for "quota exhausted" are
// automatically picked up as InsufficientResources.
func KindFromAppError(err *domain.AppError) PluginErrorKind {
	if err == nil {
		return PluginErrKindUnknown
	}
	if k, ok := err.Detail.(PluginErrorKind); ok && k != "" {
		return k
	}
	if kd, ok := err.Detail.(KindedDetail); ok {
		if k := kd.Kind(); k != "" {
			return k
		}
	}
	switch err.Code {
	case domain.ErrCodeServiceUnavailable, domain.ErrCodeTooManyRequests:
		return PluginErrKindInsufficientResources
	case domain.ErrCodeBadRequest, domain.ErrCodeUnprocessableEntity:
		return PluginErrKindInvalidSpec
	default:
		return PluginErrKindInternal
	}
}
