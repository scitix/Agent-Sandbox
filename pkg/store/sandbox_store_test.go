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
	"testing"
	"time"

	"k8s.io/utils/ptr"

	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

const (
	testSandboxID001    = "sbx-001"
	testStatusCompleted = "Completed"
)

func newTestStore(t *testing.T) SandboxStore {
	t.Helper()
	s, err := NewSandboxStore(time.Hour)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func makeRecord(t *testing.T, namespace, sandboxID, poolName, status, claimedAt string) gen.Sandbox {
	return gen.Sandbox{
		SandboxId: sandboxID,
		Namespace: namespace,
		PoolName:  poolName,
		PodName:   "pod-" + sandboxID,
		Status:    gen.SandboxStatus(status),
		ClaimedAt: mustParseRFC3339(t, claimedAt),
	}
}

func TestSandboxStore_SaveAndGet(t *testing.T) {
	s := newTestStore(t)

	record := makeRecord(t, "tenant-a", "sbx-001", "pool-a", "Completed", "2026-01-01T10:00:00Z")
	if err := s.Save(record); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get("tenant-a", "sbx-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.SandboxId != testSandboxID001 {
		t.Fatalf("expected sandboxID sbx-001, got %s", got.SandboxId)
	}
	if string(got.Status) != testStatusCompleted {
		t.Fatalf("expected status Completed, got %s", got.Status)
	}
	if got.PoolName != "pool-a" {
		t.Fatalf("expected poolName pool-a, got %s", got.PoolName)
	}
}

func TestSandboxStore_GetNotFound(t *testing.T) {
	s := newTestStore(t)

	got, err := s.Get("tenant-a", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSandboxStore_ListEmpty(t *testing.T) {
	s := newTestStore(t)

	records, err := s.List("tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

func TestSandboxStore_ListSingle(t *testing.T) {
	s := newTestStore(t)

	record := makeRecord(t, "tenant-a", "sbx-001", "pool-a", "Completed", "2026-01-01T10:00:00Z")
	if err := s.Save(record); err != nil {
		t.Fatalf("save: %v", err)
	}

	records, err := s.List("tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].SandboxId != testSandboxID001 {
		t.Fatalf("expected sbx-001, got %s", records[0].SandboxId)
	}
}

func TestSandboxStore_ListMultipleSortedByClaimedAtDesc(t *testing.T) {
	s := newTestStore(t)

	records := []gen.Sandbox{
		makeRecord(t, "tenant-a", "sbx-001", "pool-a", "Completed", "2026-01-01T08:00:00Z"),
		makeRecord(t, "tenant-a", "sbx-002", "pool-a", "Failed", "2026-01-01T10:00:00Z"),
		makeRecord(t, "tenant-a", "sbx-003", "pool-a", "Completed", "2026-01-01T09:00:00Z"),
	}
	for _, r := range records {
		if err := s.Save(r); err != nil {
			t.Fatalf("save %s: %v", r.SandboxId, err)
		}
	}

	got, err := s.List("tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}

	// Should be sorted desc: sbx-002 (10:00) > sbx-003 (09:00) > sbx-001 (08:00)
	expectedOrder := []string{"sbx-002", "sbx-003", "sbx-001"}
	for i, expectedID := range expectedOrder {
		if got[i].SandboxId != expectedID {
			t.Errorf("position %d: expected %s, got %s", i, expectedID, got[i].SandboxId)
		}
	}
}

func TestSandboxStore_NamespaceIsolation(t *testing.T) {
	s := newTestStore(t)

	if err := s.Save(makeRecord(t, "tenant-a", "sbx-001", "pool-a", "Completed", "2026-01-01T10:00:00Z")); err != nil {
		t.Fatalf("save tenant-a: %v", err)
	}
	if err := s.Save(makeRecord(t, "tenant-b", "sbx-002", "pool-b", "Failed", "2026-01-01T10:00:00Z")); err != nil {
		t.Fatalf("save tenant-b: %v", err)
	}

	recordsA, err := s.List("tenant-a")
	if err != nil {
		t.Fatalf("list tenant-a: %v", err)
	}
	if len(recordsA) != 1 {
		t.Fatalf("expected 1 record for tenant-a, got %d", len(recordsA))
	}
	if recordsA[0].SandboxId != testSandboxID001 {
		t.Fatalf("expected sbx-001 for tenant-a, got %s", recordsA[0].SandboxId)
	}

	recordsB, err := s.List("tenant-b")
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(recordsB) != 1 {
		t.Fatalf("expected 1 record for tenant-b, got %d", len(recordsB))
	}
	if recordsB[0].SandboxId != "sbx-002" {
		t.Fatalf("expected sbx-002 for tenant-b, got %s", recordsB[0].SandboxId)
	}
}

func TestSandboxStore_SaveOverwrite(t *testing.T) {
	s := newTestStore(t)

	record := makeRecord(t, "tenant-a", "sbx-001", "pool-a", "Running", "2026-01-01T10:00:00Z")
	if err := s.Save(record); err != nil {
		t.Fatalf("save first: %v", err)
	}

	// Overwrite with updated status
	record.Status = gen.SandboxStatus("Completed")
	terminatedAt := mustParseRFC3339(t, "2026-01-01T11:00:00Z")
	record.TerminatedAt = &terminatedAt
	if err := s.Save(record); err != nil {
		t.Fatalf("save second: %v", err)
	}

	got, err := s.Get("tenant-a", testSandboxID001)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Status) != testStatusCompleted {
		t.Fatalf("expected Completed after overwrite, got %s", got.Status)
	}
	if got.TerminatedAt == nil || !got.TerminatedAt.Equal(terminatedAt) {
		t.Fatalf("expected terminatedAt %v, got %v", terminatedAt, got.TerminatedAt)
	}
}

func TestSandboxStore_ShortTTLExpiry(t *testing.T) {
	// Use very short TTL to test expiry behavior
	s, err := NewSandboxStore(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer func() { _ = s.Close() }()

	record := makeRecord(t, "tenant-a", "sbx-ttl", "pool-a", "Completed", "2026-01-01T10:00:00Z")
	if err := s.Save(record); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Record should be accessible immediately
	got, err := s.Get("tenant-a", "sbx-ttl")
	if err != nil {
		t.Fatalf("get before expiry: %v", err)
	}
	if got == nil {
		t.Fatal("expected record before TTL expiry")
	}

	// Wait for TTL to expire
	time.Sleep(200 * time.Millisecond)

	got, err = s.Get("tenant-a", "sbx-ttl")
	if err != nil {
		t.Fatalf("get after expiry: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after TTL expiry")
	}
}

func TestSandboxStore_HistoricalFields(t *testing.T) {
	s := newTestStore(t)

	exitCode := int32(137)
	terminatedAt := mustParseRFC3339(t, "2026-01-01T10:30:00Z")
	record := gen.Sandbox{
		SandboxId:      "sbx-oom",
		Namespace:      "tenant-a",
		PoolName:       "pool-a",
		PodName:        "pod-abc",
		Status:         gen.SandboxStatus("Failed"),
		ClaimedAt:      mustParseRFC3339(t, "2026-01-01T10:00:00Z"),
		TerminatedAt:   &terminatedAt,
		FailureReason:  ptr.To("OOMKilled"),
		ExitCode:       &exitCode,
		FailureMessage: ptr.To("container was OOM-killed"),
	}
	if err := s.Save(record); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get("tenant-a", "sbx-oom")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.FailureReason == nil || *got.FailureReason != "OOMKilled" {
		t.Fatalf("expected OOMKilled, got %v", got.FailureReason)
	}
	if got.ExitCode == nil || *got.ExitCode != 137 {
		t.Fatalf("expected exitCode 137, got %v", got.ExitCode)
	}
	if got.TerminatedAt == nil || !got.TerminatedAt.Equal(terminatedAt) {
		t.Fatalf("expected terminatedAt %v, got %v", terminatedAt, got.TerminatedAt)
	}
}
