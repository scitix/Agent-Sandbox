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

// Package httpctx provides shared helpers for extracting authentication
// context from gin-based HTTP handlers.
//
// Both the native AgentBox API and the E2B-compatible API use oapi-codegen's
// strict server mode, which passes *gin.Context directly as context.Context.
// These helpers safely extract AuthInfo regardless of whether the context has
// been wrapped by context.WithValue.
package httpctx

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

const (
	// authContextKey matches the key used by both middleware packages.
	authContextKey = "auth"
	// defaultNamespace is used when no namespace is found.
	defaultNamespace = "default"
)

// GinFromCtx extracts the *gin.Context from a context.Context.
// Returns nil if the context is not or does not wrap a *gin.Context.
func GinFromCtx(ctx context.Context) *gin.Context {
	if gc, ok := ctx.(*gin.Context); ok {
		return gc
	}
	// When context.WithValue wraps a *gin.Context, the direct type assertion
	// fails. Fall back to gin's own ContextKey lookup, which traverses the
	// value chain and returns the original *gin.Context.
	if v := ctx.Value(gin.ContextKey); v != nil {
		if gc, ok := v.(*gin.Context); ok {
			return gc
		}
	}
	return nil
}

// AuthFrom extracts domain.AuthInfo from the context.
//
// It first tries a direct *gin.Context type assertion (fast path when the
// context has not been wrapped). If that fails, it uses ctx.Value("auth")
// which traverses through any context.WithValue layers and reaches the
// underlying gin.Context's key store.
func AuthFrom(ctx context.Context) domain.AuthInfo {
	// Fast path: ctx is a raw *gin.Context.
	if gc, ok := ctx.(*gin.Context); ok {
		return authFromGin(gc)
	}
	// Wrapped context: ctx.Value("auth") delegates through the value chain
	// to gin.Context.Value which checks gin's internal Keys map.
	if v := ctx.Value(authContextKey); v != nil {
		if auth, ok := v.(domain.AuthInfo); ok {
			if auth.Namespace == "" {
				auth.Namespace = defaultNamespace
			}
			return auth
		}
	}
	return domain.AuthInfo{Namespace: defaultNamespace}
}

func authFromGin(gc *gin.Context) domain.AuthInfo {
	value, ok := gc.Get(authContextKey)
	if !ok {
		return domain.AuthInfo{Namespace: defaultNamespace}
	}
	auth, ok := value.(domain.AuthInfo)
	if !ok {
		return domain.AuthInfo{Namespace: defaultNamespace}
	}
	if auth.Namespace == "" {
		auth.Namespace = defaultNamespace
	}
	return auth
}
