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

package store

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/buntdb"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

const defaultTTL = 24 * time.Hour

// SandboxStore is the interface for persisting historical Sandbox records.
type SandboxStore interface {
	// Save persists a sandbox with the configured TTL.
	Save(sandbox gen.Sandbox) error
	// Get retrieves a sandbox by namespace+sandboxID. Returns nil, nil if not found.
	Get(namespace, sandboxID string) (*gen.Sandbox, error)
	// List returns all sandboxes for the given namespace, sorted by claimedAt desc.
	List(namespace string) ([]gen.Sandbox, error)
	// Close closes the underlying store.
	Close() error
}

// buntdbSandboxStore implements SandboxStore using buntdb.
type buntdbSandboxStore struct {
	db  *buntdb.DB
	ttl time.Duration
}

// NewSandboxStore creates an in-memory buntdb SandboxStore with the given TTL.
// If ttl <= 0, defaultTTL (24h) is used.
func NewSandboxStore(ttl time.Duration) (SandboxStore, error) {
	if ttl <= 0 {
		ttl = defaultTTL
	}

	db, err := buntdb.Open(":memory:")
	if err != nil {
		return nil, err
	}

	return &buntdbSandboxStore{
		db:  db,
		ttl: ttl,
	}, nil
}

// storeKey returns the buntdb key for a given namespace+sandboxID.
func storeKey(namespace, sandboxID string) string {
	return namespace + "/" + sandboxID
}

// Save persists a gen.Sandbox with the configured TTL. Runtime-only fields
// (Endpoints, StatusDetail, DurationSeconds) on a historical record are
// expected to be unset by the writer; they are serialized as-is.
func (s *buntdbSandboxStore) Save(sandbox gen.Sandbox) error {
	data, err := json.Marshal(sandbox)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *buntdb.Tx) error {
		key := storeKey(sandbox.Namespace, sandbox.SandboxId)
		_, _, err := tx.Set(key, string(data), &buntdb.SetOptions{
			Expires: true,
			TTL:     s.ttl,
		})
		return err
	})
}

// Get retrieves a gen.Sandbox by namespace+sandboxID. Returns nil, nil if not found.
func (s *buntdbSandboxStore) Get(namespace, sandboxID string) (*gen.Sandbox, error) {
	var result *gen.Sandbox

	err := s.db.View(func(tx *buntdb.Tx) error {
		key := storeKey(namespace, sandboxID)
		val, err := tx.Get(key)
		if err == buntdb.ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}

		var sb gen.Sandbox
		if err := json.Unmarshal([]byte(val), &sb); err != nil {
			return err
		}
		result = &sb
		return nil
	})

	return result, err
}

// List returns all sandboxes for the given namespace, sorted by claimedAt descending.
func (s *buntdbSandboxStore) List(namespace string) ([]gen.Sandbox, error) {
	prefix := namespace + "/"
	var sandboxes []gen.Sandbox

	err := s.db.View(func(tx *buntdb.Tx) error {
		return tx.AscendKeys(prefix+"*", func(key, val string) bool {
			// Extra safety: verify prefix matches (buntdb pattern matching uses glob-style)
			if !strings.HasPrefix(key, prefix) {
				return true
			}

			var sb gen.Sandbox
			if err := json.Unmarshal([]byte(val), &sb); err != nil {
				return true // skip malformed records
			}
			sandboxes = append(sandboxes, sb)
			return true
		})
	})
	if err != nil {
		return nil, err
	}

	// Sort by claimedAt descending (most recent first)
	sort.Slice(sandboxes, func(i, j int) bool {
		return sandboxes[i].ClaimedAt.After(sandboxes[j].ClaimedAt)
	})

	return sandboxes, nil
}

// Close closes the underlying buntdb instance.
func (s *buntdbSandboxStore) Close() error {
	return s.db.Close()
}
