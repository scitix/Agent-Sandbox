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
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/utils/ptr"

	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

// A stream request for a sandbox that has ended has to be answered from the
// central log service, the same as the non-streaming endpoint.
//
// For a long time it was not. The streaming handler tried the live Pod, found
// none, and closed the stream with a bare `{"_meta":true,"source":"live"}` —
// so the one endpoint the console actually calls for logs reported nothing at
// all for every finished sandbox, while `GET /logs` on the same sandbox
// returned the lines. Nothing errored; the logs were simply unreachable by the
// path anyone used.

// The meta line's source for a finished sandbox, and the thing that tells a
// reader the answer came from the log service rather than from a live Pod.
const sourceCentral = "central"

type fakeLogReader struct {
	result *gen.SandboxLogsResult
	err    *domain.AppError

	gotNamespace string
	gotSandboxID string
	gotParams    gen.GetSandboxLogsParams
}

func (f *fakeLogReader) GetLogs(
	_ context.Context, namespace, sandboxID string, params gen.GetSandboxLogsParams,
) (*gen.SandboxLogsResult, *domain.AppError) {
	f.gotNamespace, f.gotSandboxID, f.gotParams = namespace, sandboxID, params
	return f.result, f.err
}

// runCentral drives the helper and splits the NDJSON it wrote.
func runCentral(t *testing.T, svc historicalLogReader, container string, lines int) (
	entries []streamLogEntry, meta ndjsonMetaLine,
) {
	t.Helper()
	var buf bytes.Buffer
	var gotMeta ndjsonMetaLine
	writeMeta := func(src string, truncated bool, podName string) {
		gotMeta = ndjsonMetaLine{Meta: true, Source: src, Truncated: truncated, PodName: podName}
	}

	streamCentralLogsToEnc(
		context.Background(), svc, "t-team-a", "sbx-1", container, lines,
		json.NewEncoder(&buf), writeMeta,
	)

	for raw := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var e streamLogEntry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Fatalf("line is not valid NDJSON: %v (%q)", err, raw)
		}
		entries = append(entries, e)
	}
	return entries, gotMeta
}

func TestFinishedSandboxLogsComeBackOnTheStream(t *testing.T) {
	ts := time.Date(2026, 9, 15, 3, 31, 58, 0, time.UTC)
	svc := &fakeLogReader{result: &gen.SandboxLogsResult{
		SandboxId: "sbx-1",
		Namespace: "t-team-a",
		PodName:   ptr.To("pool-abc-xyz12"),
		Source:    gen.SandboxLogsResultSource(sourceCentral),
		Entries: []gen.SandboxLogEntry{
			{Container: "sandbox", Log: "first", Timestamp: &ts},
			{Container: "sandbox", Log: "second"},
		},
	}}

	entries, meta := runCentral(t, svc, "", 0)

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Log != "first" || entries[1].Log != "second" {
		t.Errorf("entries out of order or wrong: %+v", entries)
	}
	// The viewer keys its columns off these, and a live line carries them.
	if entries[0].PodName != "pool-abc-xyz12" || entries[0].NamespaceName != "t-team-a" {
		t.Errorf("entry missing pod/namespace: %+v", entries[0])
	}
	if entries[0].Timestamp == nil || !entries[0].Timestamp.Equal(ts) {
		t.Errorf("timestamp did not survive: %+v", entries[0].Timestamp)
	}
	// source distinguishes this from a live stream that simply ended.
	if meta.Source != sourceCentral || meta.PodName != "pool-abc-xyz12" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestStreamPassesContainerAndLineLimitThrough(t *testing.T) {
	// Both are the caller's, and dropping either silently changes the answer:
	// the wrong container buries the sandbox's output under the egress proxy's
	// per-connection lines, and an ignored limit returns the whole run.
	svc := &fakeLogReader{result: &gen.SandboxLogsResult{Entries: nil}}

	runCentral(t, svc, "egress-proxy", 50)

	if svc.gotParams.Container == nil || *svc.gotParams.Container != "egress-proxy" {
		t.Errorf("container not forwarded: %+v", svc.gotParams.Container)
	}
	if svc.gotParams.Lines == nil || *svc.gotParams.Lines != 50 {
		t.Errorf("lines not forwarded: %+v", svc.gotParams.Lines)
	}
	if svc.gotNamespace != "t-team-a" || svc.gotSandboxID != "sbx-1" {
		t.Errorf("wrong sandbox asked about: %s/%s", svc.gotNamespace, svc.gotSandboxID)
	}
}

func TestUnsetContainerAndLinesAreLeftUnset(t *testing.T) {
	// The service defaults both, and sending 0 lines would mean "all" to one
	// reader and "none" to another. Leave the choice where it belongs.
	svc := &fakeLogReader{result: &gen.SandboxLogsResult{Entries: nil}}

	runCentral(t, svc, "", 0)

	if svc.gotParams.Container != nil {
		t.Errorf("container should be unset, got %q", *svc.gotParams.Container)
	}
	if svc.gotParams.Lines != nil {
		t.Errorf("lines should be unset, got %d", *svc.gotParams.Lines)
	}
}

func TestAnEmptyResultReportsWhatWasAsked(t *testing.T) {
	// The whole point of the scope string. A blank panel is the same pixels
	// whether the sandbox printed nothing or the query went to the wrong log
	// store, and only one of those is worth investigating.
	svc := &fakeLogReader{result: &gen.SandboxLogsResult{
		Entries: nil,
		Scope:   ptr.To("project=internal filters=[cluster=prod-foo pod_name=p-1] window=..."),
	}}

	entries, meta := runCentral(t, svc, "", 0)

	if len(entries) != 1 {
		t.Fatalf("want one explanatory line, got %+v", entries)
	}
	if entries[0].ContainerName != "system" {
		t.Errorf("the explanation must be marked as ours, got %q", entries[0].ContainerName)
	}
	if !strings.Contains(entries[0].Log, "project=internal") {
		t.Errorf("the scope did not reach the reader: %q", entries[0].Log)
	}
	if meta.Source != sourceCentral {
		t.Errorf("meta = %+v", meta)
	}
}

func TestAnEmptyResultWithNoScopeStaysQuiet(t *testing.T) {
	// Nothing useful to say, so say nothing rather than emit a line that
	// renders as an empty log entry.
	svc := &fakeLogReader{result: &gen.SandboxLogsResult{Entries: nil}}

	entries, meta := runCentral(t, svc, "", 0)

	if len(entries) != 0 {
		t.Errorf("expected no lines, got %+v", entries)
	}
	if meta.Source != sourceCentral {
		t.Errorf("meta = %+v", meta)
	}
}

func TestAServiceErrorIsTheAnswer(t *testing.T) {
	// The status code was committed to 200 before the first byte, so an error
	// discovered afterwards can only be delivered in the stream. "No central
	// log service configured" is a deployment gap someone has to see.
	svc := &fakeLogReader{err: domain.NewNotFound(
		"sandbox t-team-a/sbx-1 has ended and its pod was recycled; historical logs are unavailable")}

	entries, meta := runCentral(t, svc, "", 0)

	if len(entries) != 1 || entries[0].ContainerName != "system" {
		t.Fatalf("the error did not reach the stream: %+v", entries)
	}
	if !strings.Contains(entries[0].Log, "pod was recycled") {
		t.Errorf("message lost: %q", entries[0].Log)
	}
	if meta.Source != sourceCentral {
		t.Errorf("meta = %+v", meta)
	}
}

func TestTruncationIsReported(t *testing.T) {
	// The viewer renders a "results were cut off" marker from this; losing it
	// presents a partial run as a complete one.
	svc := &fakeLogReader{result: &gen.SandboxLogsResult{
		PodName:   ptr.To("p-1"),
		Truncated: true,
		Entries:   []gen.SandboxLogEntry{{Container: "sandbox", Log: "x"}},
	}}

	_, meta := runCentral(t, svc, "", 0)

	if !meta.Truncated {
		t.Error("truncation did not reach the meta line")
	}
}
