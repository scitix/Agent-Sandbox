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

package sandboxpool

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

var testKey = types.NamespacedName{Namespace: "default", Name: "pool-a"}

func TestPoolExpectations_SatisfiedOnEmpty(t *testing.T) {
	e := NewPoolExpectations()
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true for unknown key")
	}
}

func TestPoolExpectations_ExpectCreations_BlocksUntilObserved(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectCreations(testKey, 3)

	if e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=false after ExpectCreations(3)")
	}

	e.CreationObserved(testKey)
	e.CreationObserved(testKey)
	if e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=false after only 2 of 3 observed")
	}

	e.CreationObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true after all 3 observed")
	}
}

func TestPoolExpectations_ExpectDeletions_BlocksUntilObserved(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectDeletions(testKey, 2)

	if e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=false after ExpectDeletions(2)")
	}

	e.DeletionObserved(testKey)
	if e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=false after only 1 of 2 observed")
	}

	e.DeletionObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true after both deletions observed")
	}
}

func TestPoolExpectations_CreationObserved_ClampedAtZero(t *testing.T) {
	e := NewPoolExpectations()
	// Call CreationObserved without any prior ExpectCreations — should not panic or go negative.
	e.CreationObserved(testKey)
	e.CreationObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true: counter must not go below zero")
	}
}

func TestPoolExpectations_DeletionObserved_ClampedAtZero(t *testing.T) {
	e := NewPoolExpectations()
	e.DeletionObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true: counter must not go below zero")
	}
}

func TestPoolExpectations_ExpectCreations_ReplacesExistingCount(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectCreations(testKey, 10)
	// New scale decision replaces the old count.
	e.ExpectCreations(testKey, 2)

	e.CreationObserved(testKey)
	e.CreationObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true: new count of 2 was fully observed")
	}
}

func TestPoolExpectations_CreationsAndDeletionsAreIndependent(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectCreations(testKey, 1)
	e.ExpectDeletions(testKey, 1)

	// Satisfy creations only — deletions still pending.
	e.CreationObserved(testKey)
	if e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=false: deletions still pending")
	}

	// Satisfy deletions — now both zero.
	e.DeletionObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true after both creations and deletions observed")
	}
}

func TestPoolExpectations_ExpectDeletions_PreservesExistingCreations(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectCreations(testKey, 2)
	// Setting deletions must not wipe the pending creations.
	e.ExpectDeletions(testKey, 1)

	e.DeletionObserved(testKey)
	if e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=false: 2 pending creations still outstanding")
	}

	e.CreationObserved(testKey)
	e.CreationObserved(testKey)
	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true after all pending ops observed")
	}
}

func TestPoolExpectations_TTLExpiry(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectCreations(testKey, 5)

	// Manually backdate the timestamp to simulate TTL expiry.
	e.mu.Lock()
	e.items[testKey].timestamp = time.Now().Add(-(expectationsTTL + time.Second))
	e.mu.Unlock()

	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true after TTL expiry")
	}
	// Entry should be cleaned up.
	e.mu.Lock()
	_, exists := e.items[testKey]
	e.mu.Unlock()
	if exists {
		t.Fatal("expected stale entry to be deleted after TTL expiry")
	}
}

func TestPoolExpectations_DeleteExpectations(t *testing.T) {
	e := NewPoolExpectations()
	e.ExpectCreations(testKey, 5)
	e.DeleteExpectations(testKey)

	if !e.Satisfied(testKey) {
		t.Fatal("expected Satisfied=true after DeleteExpectations")
	}
}

func TestPoolExpectations_MultipleKeys(t *testing.T) {
	keyA := types.NamespacedName{Namespace: "ns", Name: "pool-a"}
	keyB := types.NamespacedName{Namespace: "ns", Name: "pool-b"}

	e := NewPoolExpectations()
	e.ExpectCreations(keyA, 3)
	e.ExpectCreations(keyB, 1)

	e.CreationObserved(keyB)
	// keyB satisfied, keyA still pending.
	if !e.Satisfied(keyB) {
		t.Fatal("expected keyB Satisfied=true")
	}
	if e.Satisfied(keyA) {
		t.Fatal("expected keyA Satisfied=false")
	}

	e.CreationObserved(keyA)
	e.CreationObserved(keyA)
	e.CreationObserved(keyA)
	if !e.Satisfied(keyA) {
		t.Fatal("expected keyA Satisfied=true after all observed")
	}
}
