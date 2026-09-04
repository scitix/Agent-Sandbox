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

package handlers

import (
	"testing"
	"time"

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

func TestParseLogLevel(t *testing.T) {
	for _, tc := range []struct {
		line string
		want e2bgen.LogLevel
	}{
		{"ERROR failed to connect", e2bgen.LogLevelError},
		{"[WARN] retrying", e2bgen.LogLevelWarn},
		{"DEBUG: cache hit", e2bgen.LogLevelDebug},
		{"warning: deprecated flag", e2bgen.LogLevelWarn},
		{"fatal: out of memory", e2bgen.LogLevelError},
		// Anything unrecognised is info. Container output is unstructured, and
		// guessing harder would make the level look more reliable than it is.
		{"just some output", e2bgen.LogLevelInfo},
		{"", e2bgen.LogLevelInfo},
		// A severity word in the middle of a line is not a level.
		{"the ERROR count is 0", e2bgen.LogLevelInfo},
	} {
		if got := parseLogLevel(tc.line); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.line, got, tc.want)
		}
	}
}

func TestLogQuery_LevelFilterIsAMinimum(t *testing.T) {
	warn := e2bgen.LogLevelWarn
	q := logQuery{minLevel: &warn}

	if q.matches(e2bgen.SandboxLogEntry{Level: e2bgen.LogLevelInfo}) {
		t.Error("info is below the minimum and must be excluded")
	}
	if !q.matches(e2bgen.SandboxLogEntry{Level: e2bgen.LogLevelWarn}) {
		t.Error("the minimum itself must be included")
	}
	if !q.matches(e2bgen.SandboxLogEntry{Level: e2bgen.LogLevelError}) {
		t.Error("above the minimum must be included")
	}
}

func TestLogQuery_SearchIsCaseSensitive(t *testing.T) {
	needle := "Timeout"
	q := logQuery{search: &needle}

	if !q.matches(e2bgen.SandboxLogEntry{Message: "request Timeout after 30s"}) {
		t.Error("expected a substring match")
	}
	// Upstream's filter is case-sensitive; matching loosely here would make the
	// same query return different results against the two backends.
	if q.matches(e2bgen.SandboxLogEntry{Message: "request timeout after 30s"}) {
		t.Error("search must be case-sensitive")
	}
}

func TestLogQuery_StartExcludesOlderLines(t *testing.T) {
	cutoff := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	ms := cutoff.UnixMilli()
	q := logQuery{startMillis: &ms}

	if q.matches(e2bgen.SandboxLogEntry{Timestamp: cutoff.Add(-time.Second)}) {
		t.Error("a line older than start must be excluded")
	}
	if !q.matches(e2bgen.SandboxLogEntry{Timestamp: cutoff.Add(time.Second)}) {
		t.Error("a line newer than start must be included")
	}
	// A line with no timestamp cannot be placed, and dropping it would lose
	// output for no reason; keep it.
	if !q.matches(e2bgen.SandboxLogEntry{}) {
		t.Error("an undated line must not be dropped by a start filter")
	}
}
