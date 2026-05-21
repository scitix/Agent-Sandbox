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

package service

import (
	"sync"
	"time"
)

// ExecTokenInfo carries the information stored in an exec token.
// It is returned by ValidateExecToken after successful token validation.
type ExecTokenInfo struct {
	SandboxID  string
	Namespace  string
	PodName    string
	Containers []string
}

const (
	execTokenTTL     = 30 * time.Second
	execTokenGCEvery = 5 * time.Second
)

// ExecTokenRecord holds the information associated with a one-time exec token.
type ExecTokenRecord struct {
	SandboxID  string
	Namespace  string
	PodName    string
	Containers []string
	ExpiresAt  time.Time
}

// execTokenStore is a thread-safe store for short-lived exec tokens.
// Tokens are invalidated on first use and expire after execTokenTTL.
type execTokenStore struct {
	m sync.Map // string → *ExecTokenRecord
}

// newExecTokenStore creates a new store and starts the GC goroutine.
// The GC goroutine runs until done is closed.
func newExecTokenStore(done <-chan struct{}) *execTokenStore {
	s := &execTokenStore{}
	go s.gc(done)
	return s
}

// Set stores a token with the given record.
func (s *execTokenStore) Set(token string, rec ExecTokenRecord) {
	s.m.Store(token, &rec)
}

// Consume validates and atomically removes the token. Returns nil if the token
// is missing or expired.
func (s *execTokenStore) Consume(token string) *ExecTokenInfo {
	val, loaded := s.m.LoadAndDelete(token)
	if !loaded {
		return nil
	}
	rec := val.(*ExecTokenRecord)
	if time.Now().After(rec.ExpiresAt) {
		return nil
	}
	return &ExecTokenInfo{
		SandboxID:  rec.SandboxID,
		Namespace:  rec.Namespace,
		PodName:    rec.PodName,
		Containers: rec.Containers,
	}
}

// gc periodically removes expired tokens from the store.
func (s *execTokenStore) gc(done <-chan struct{}) {
	ticker := time.NewTicker(execTokenGCEvery)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			now := time.Now()
			s.m.Range(func(key, value any) bool {
				rec := value.(*ExecTokenRecord)
				if now.After(rec.ExpiresAt) {
					s.m.Delete(key)
				}
				return true
			})
		}
	}
}
