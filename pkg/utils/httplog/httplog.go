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

// Package httplog centralizes HTTP request logging for the AgentBox API
// servers. It provides a request-ID middleware, structured error logging
// keyed off domain.AppError, and a gin log formatter that includes the
// request ID and any error attached via c.Error.
//
// The design goal is that every 5xx response a client sees has a matching
// ERROR-level log line on the server, with enough context (method, path,
// request ID, underlying cause) to diagnose the failure without enabling
// request-body capture.
package httplog

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"k8s.io/klog/v2"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

const (
	// RequestIDHeader is the HTTP header clients may send to set the request
	// ID. When absent a UUID v7 is generated server-side.
	RequestIDHeader = "X-Request-ID"
	// requestIDContextKey is the gin.Context key under which the request ID
	// is stored so downstream handlers (and the gin logger) can read it.
	requestIDContextKey = "request_id"
)

// RequestIDFromGin returns the request ID attached to the gin context by
// RequestID middleware. Empty when the middleware was not installed.
func RequestIDFromGin(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(requestIDContextKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// RequestID returns a middleware that assigns each request a UUID v7 ID and
// echoes it back in the X-Request-ID response header. Existing inbound IDs
// (e.g. from an upstream gateway) are preserved so logs can be correlated
// across hops.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			if u, err := uuid.NewV7(); err == nil {
				id = u.String()
			} else {
				id = uuid.NewString()
			}
		}
		c.Set(requestIDContextKey, id)
		c.Writer.Header().Set(RequestIDHeader, id)
		c.Next()
	}
}

// LogAppError emits a klog entry for an AppError returned from the service
// layer. 5xx-class codes are logged at ERROR level with the underlying
// Cause; 4xx-class codes are logged at V(4) to avoid flooding production
// logs with client-side validation failures.
//
// c may be nil (e.g. when called from a non-gin code path); the helper then
// falls back to logging without request metadata.
func LogAppError(c *gin.Context, appErr *domain.AppError) {
	if appErr == nil {
		return
	}
	code := int(appErr.Code)
	fields := []any{"code", code}
	if appErr.BizCode != "" {
		fields = append(fields, "bizCode", string(appErr.BizCode))
	}
	if c != nil {
		fields = append(fields,
			"method", c.Request.Method,
			"path", c.FullPath(),
			"requestID", RequestIDFromGin(c),
		)
	}
	if code >= 500 {
		klog.ErrorS(appErr.Cause, appErr.Message, fields...)
		if c != nil {
			// Attach to gin so the access log line for this request surfaces
			// the error message (via GinLogFormatter) without a second pass.
			_ = c.Error(appErr)
		}
		return
	}
	// 4xx: only log at high verbosity to aid debugging. These are caller
	// errors and we don't want them to dominate normal operator logs.
	if klogV := klog.V(4); klogV.Enabled() {
		klogV.InfoS("client error response", append(fields, "message", appErr.Message)...)
	}
}

// LogServerError logs an unexpected server-side error that did not go
// through the AppError abstraction (e.g. a JSON marshal failure inside a
// handler). Always logged at ERROR level. c may be nil.
func LogServerError(c *gin.Context, err error, msg string, keysAndValues ...any) {
	if err == nil {
		return
	}
	fields := append([]any{}, keysAndValues...)
	if c != nil {
		fields = append(fields,
			"method", c.Request.Method,
			"path", c.FullPath(),
			"requestID", RequestIDFromGin(c),
		)
		_ = c.Error(err)
	}
	klog.ErrorS(err, msg, fields...)
}

// GinLogFormatter returns a gin.LogFormatter that prefixes each line with
// the given tag (e.g. "[RAW]" for the native API, "[E2B]" for the E2B
// compat API) and includes the request ID plus any errors attached to the
// request via c.Error (typically by LogAppError).
//
// The request ID is taken from the X-Request-ID response header, which the
// RequestID middleware sets on every request.
func GinLogFormatter(prefix string) gin.LogFormatter {
	return func(param gin.LogFormatterParams) string {
		var statusColor, methodColor, resetColor string
		if param.IsOutputColor() {
			statusColor = param.StatusCodeColor()
			methodColor = param.MethodColor()
			resetColor = param.ResetColor()
		}
		if param.Latency > time.Minute {
			param.Latency = param.Latency.Truncate(time.Second)
		}
		rid := ""
		if param.Keys != nil {
			if v, ok := param.Keys[requestIDContextKey]; ok {
				if s, ok := v.(string); ok {
					rid = s
				}
			}
		}
		errSuffix := ""
		if param.ErrorMessage != "" {
			errSuffix = " err=" + strconv.Quote(param.ErrorMessage)
		}
		return fmt.Sprintf("%s %v |%s %3d %s| %13v | %15s | rid=%s |%s %-7s %s %#v%s\n",
			prefix,
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			statusColor, param.StatusCode, resetColor,
			param.Latency,
			param.ClientIP,
			rid,
			methodColor, param.Method, resetColor,
			param.Path,
			errSuffix,
		)
	}
}
