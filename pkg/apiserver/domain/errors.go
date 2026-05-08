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

// ErrorCode maps to HTTP status codes.
type ErrorCode int

const (
	ErrCodeBadRequest          ErrorCode = 400
	ErrCodeUnauthorized        ErrorCode = 401
	ErrCodeForbidden           ErrorCode = 403
	ErrCodeNotFound            ErrorCode = 404
	ErrCodeConflict            ErrorCode = 409
	ErrCodeUnprocessableEntity ErrorCode = 422
	ErrCodeTooManyRequests     ErrorCode = 429
	ErrCodeServiceUnavailable  ErrorCode = 503
	ErrCodeInternal            ErrorCode = 500
)

// BusinessErrorCode is a machine-readable business error code carried alongside
// specific AppErrors that require special frontend handling (e.g. redirecting the
// user to another page instead of showing a generic toast). It is intentionally
// orthogonal to the HTTP status code: the same HTTP status can carry different
// BusinessErrorCodes depending on the scenario.
type BusinessErrorCode string

const (
	// BizErrAPIKeyRequired indicates the current user has no API Key and must
	// create one before performing this operation. The frontend should navigate
	// the user to the API Key management page rather than showing a generic error.
	BizErrAPIKeyRequired BusinessErrorCode = "API_KEY_REQUIRED"
)

// AppError is a domain-level error that carries an HTTP-status-mapped code, a
// user-visible message, an optional wrapped cause for logging, and optional
// structured detail for the API response.
type AppError struct {
	Code    ErrorCode
	BizCode BusinessErrorCode // optional; when non-empty, serialised into the errorCode response field
	Message string
	Cause   error // not exposed to callers; used for logging
	Detail  any   // structured extra info (e.g. PoolStatusDetail), serialized in response
}

func (e *AppError) Error() string { return e.Message }
func (e *AppError) Unwrap() error { return e.Cause }

// NewNotFound constructs a 404 AppError.
func NewNotFound(msg string) *AppError {
	return &AppError{Code: ErrCodeNotFound, Message: msg}
}

// NewConflict constructs a 409 AppError.
func NewConflict(msg string) *AppError {
	return &AppError{Code: ErrCodeConflict, Message: msg}
}

// NewBadRequest constructs a 400 AppError.
func NewBadRequest(msg string) *AppError {
	return &AppError{Code: ErrCodeBadRequest, Message: msg}
}

// NewInternal constructs a 500 AppError with an underlying cause.
func NewInternal(msg string, cause error) *AppError {
	return &AppError{Code: ErrCodeInternal, Message: msg, Cause: cause}
}

// NewForbidden constructs a 403 AppError.
func NewForbidden(msg string) *AppError {
	return &AppError{Code: ErrCodeForbidden, Message: msg}
}

// NewTooManyRequests constructs a 429 AppError with an underlying cause.
func NewTooManyRequests(msg string, cause error, detail any) *AppError {
	return &AppError{Code: ErrCodeTooManyRequests, Message: msg, Cause: cause, Detail: detail}
}

// NewServiceUnavailable constructs a 503 AppError.
func NewServiceUnavailable(msg string) *AppError {
	return &AppError{Code: ErrCodeServiceUnavailable, Message: msg}
}

// NewUnauthorized constructs a 401 AppError.
func NewUnauthorized(msg string) *AppError {
	return &AppError{Code: ErrCodeUnauthorized, Message: msg}
}

// NewAPIKeyRequired constructs a 422 AppError carrying BizErrAPIKeyRequired.
// Use this when the current user has no API Key and must create one before
// the requested operation can proceed.
func NewAPIKeyRequired(msg string) *AppError {
	return &AppError{
		Code:    ErrCodeUnprocessableEntity,
		BizCode: BizErrAPIKeyRequired,
		Message: msg,
	}
}

// PoolStatusDetail is attached to 409 Conflict when creating sandboxes and
// the pool has no idle pods available.
type PoolStatusDetail struct {
	Idle       int32  `json:"idle"`
	Running    int32  `json:"running"`
	Starting   int32  `json:"starting"`
	Stopping   int32  `json:"stopping"`
	Failed     int32  `json:"failed"`
	Hint       string `json:"hint,omitempty"`
	RetryAfter int    `json:"retryAfter,omitempty"` // seconds
}

// AvailablePoolSummary is a lightweight pool descriptor included in the
// discovery detail when a client references a missing pool. It intentionally
// avoids leaking full pool spec to keep the error response compact.
type AvailablePoolSummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Idle      int32  `json:"idle"`
	Running   int32  `json:"running"`
	Starting  int32  `json:"starting"`
}

// AvailablePoolsDetail is attached to 404 Not Found when CreateSandbox
// references a pool that does not exist. The caller can pick a pool from
// AvailablePools and retry without a round-trip to ListSandboxPools.
type AvailablePoolsDetail struct {
	AvailablePools []AvailablePoolSummary `json:"availablePools"`
	Hint           string                 `json:"hint,omitempty"`
}

// AvailableTemplateSummary is a lightweight template descriptor included in
// the discovery detail when a client references a missing template.
type AvailableTemplateSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SyncSource  string `json:"syncSource,omitempty"`
}

// AvailableTemplatesDetail is attached to 404 Not Found when CreateSandboxPool
// references a template that does not exist.
type AvailableTemplatesDetail struct {
	AvailableTemplates []AvailableTemplateSummary `json:"availableTemplates"`
	Hint               string                     `json:"hint,omitempty"`
}

// AvailableQuotaURLSummary describes one quota that the caller may reference
// via spec.reservation.quotaURL.
type AvailableQuotaURLSummary struct {
	URL   string `json:"url"`
	Queue string `json:"queue,omitempty"`
	Free  string `json:"free,omitempty"`
}

// AvailableQuotaURLsDetail is attached to 400 Bad Request when a pool requests
// reservation but does not specify a quotaURL.
type AvailableQuotaURLsDetail struct {
	AvailableQuotaURLs []AvailableQuotaURLSummary `json:"availableQuotaURLs"`
	Hint               string                     `json:"hint,omitempty"`
}
