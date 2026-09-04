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
	"context"
	"regexp"
	"strings"

	apidomain "github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
	"github.com/scitix/agent-sandbox/pkg/utils/httpctx"
)

// Sandbox logs, served from the container's stdout/stderr through the
// Kubernetes log API — the same source the native endpoint uses.
//
// Container output is unstructured, and this does not pretend otherwise. The
// level is a best-effort read of the line's own prefix and defaults to info;
// the filter operates on that reading. Inventing structure that is not in the
// data would make the filter look reliable when it is not, which is worse than
// a documented approximation.

// defaultLogLimit matches the upstream default page size.
const defaultLogLimit = 1000

// levelRe matches a severity token at the start of a line, optionally inside
// brackets or followed by a colon — the shapes most loggers actually emit.
var levelRe = regexp.MustCompile(`^[\[\(<]?(TRACE|DEBUG|INFO|INFORMATION|NOTICE|WARN|WARNING|ERROR|ERR|FATAL|CRITICAL)[\]\)>:]?\b`)

// parseLogLevel reads a level from a log line, defaulting to info.
func parseLogLevel(line string) e2bgen.LogLevel {
	trimmed := strings.TrimSpace(line)
	m := levelRe.FindStringSubmatch(strings.ToUpper(trimmed))
	if m == nil {
		return e2bgen.LogLevelInfo
	}
	switch m[1] {
	case "TRACE", "DEBUG":
		return e2bgen.LogLevelDebug
	case "WARN", "WARNING":
		return e2bgen.LogLevelWarn
	case "ERROR", "ERR", "FATAL", "CRITICAL":
		return e2bgen.LogLevelError
	default:
		return e2bgen.LogLevelInfo
	}
}

// logLevelRank orders levels so a minimum-level filter can be applied.
func logLevelRank(l e2bgen.LogLevel) int {
	switch l {
	case e2bgen.LogLevelDebug:
		return 0
	case e2bgen.LogLevelInfo:
		return 1
	case e2bgen.LogLevelWarn:
		return 2
	case e2bgen.LogLevelError:
		return 3
	default:
		return 1
	}
}

// logQuery is the parsed, transport-independent form of both logs endpoints'
// query parameters.
type logQuery struct {
	startMillis *int64
	limit       int
	backward    bool
	minLevel    *e2bgen.LogLevel
	search      *string
}

func (q logQuery) matches(entry e2bgen.SandboxLogEntry) bool {
	if q.minLevel != nil && logLevelRank(entry.Level) < logLevelRank(*q.minLevel) {
		return false
	}
	if q.search != nil && *q.search != "" && !strings.Contains(entry.Message, *q.search) {
		// Case-sensitive, matching the upstream filter.
		return false
	}
	if q.startMillis != nil && !entry.Timestamp.IsZero() {
		if entry.Timestamp.UnixMilli() < *q.startMillis {
			return false
		}
	}
	return true
}

// fetchLogEntries loads a sandbox's log lines and applies the query.
func (s *Server) fetchLogEntries(ctx context.Context, sandboxID string, q logQuery) ([]e2bgen.SandboxLogEntry, *apidomain.AppError) {
	auth := authFrom(ctx)

	// Bound the fetch by the page size: pod logs of a long-lived sandbox are
	// unbounded, and pulling all of them to drop most would be paid for on
	// every call.
	lines := q.limit
	if lines <= 0 || lines > defaultLogLimit {
		lines = defaultLogLimit
	}

	result, appErr := s.sandbox.GetLogs(ctx, auth.Namespace, sandboxID, gen.GetSandboxLogsParams{
		Lines: &lines,
	})
	if appErr != nil {
		return nil, appErr
	}

	entries := make([]e2bgen.SandboxLogEntry, 0, len(result.Entries))
	for i := range result.Entries {
		src := result.Entries[i]
		entry := e2bgen.SandboxLogEntry{
			Message: src.Log,
			Level:   parseLogLevel(src.Log),
			Fields: map[string]string{
				"container": src.Container,
				"source":    "stdout",
			},
		}
		if src.Timestamp != nil {
			entry.Timestamp = *src.Timestamp
		}
		if !q.matches(entry) {
			continue
		}
		entries = append(entries, entry)
	}

	if q.backward {
		// Newest first.
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
	}
	if q.limit > 0 && len(entries) > q.limit {
		entries = entries[:q.limit]
	}
	return entries, nil
}

// logsUnavailable reports that this deployment cannot serve logs.
func (s *Server) logsUnavailable(op string) unsupported {
	return unsupportedOp(op, catUnimplemented, msgLogs)
}

func (s *Server) GetSandboxesSandboxIDLogs(ctx context.Context, req e2bgen.GetSandboxesSandboxIDLogsRequestObject) (e2bgen.GetSandboxesSandboxIDLogsResponseObject, error) {
	if s.sandbox == nil {
		return s.logsUnavailable("GetSandboxesSandboxIDLogs"), nil
	}
	if clusterID, _ := cluster.SplitSandboxID(req.SandboxID); s.isCrossCluster(clusterID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), clusterID, service.URLKindE2B, nil)
		return nil, nil
	}

	q := logQuery{limit: defaultLogLimit}
	if req.Params.Start != nil {
		q.startMillis = req.Params.Start
	}
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		q.limit = int(*req.Params.Limit)
	}

	entries, appErr := s.fetchLogEntries(ctx, req.SandboxID, q)
	if appErr != nil {
		if appErr.Code == apidomain.ErrCodeNotFound {
			return e2bgen.GetSandboxesSandboxIDLogs404JSONResponse{
				N404JSONResponse: e2bgen.N404JSONResponse(errRespCode(404, appErr.Message))}, nil
		}
		return e2bgen.GetSandboxesSandboxIDLogs500JSONResponse{
			N500JSONResponse: e2bgen.N500JSONResponse(errRespAppErr(ctx, appErr))}, nil
	}

	// The v1 shape carries the same lines twice: a flat list and a structured
	// one. Both are populated so either SDK generation reads them.
	logs := make([]e2bgen.SandboxLog, 0, len(entries))
	for _, e := range entries {
		logs = append(logs, e2bgen.SandboxLog{Timestamp: e.Timestamp, Line: e.Message})
	}
	return e2bgen.GetSandboxesSandboxIDLogs200JSONResponse{
		Logs:       logs,
		LogEntries: entries,
	}, nil
}

func (s *Server) GetV2SandboxesSandboxIDLogs(ctx context.Context, req e2bgen.GetV2SandboxesSandboxIDLogsRequestObject) (e2bgen.GetV2SandboxesSandboxIDLogsResponseObject, error) {
	if s.sandbox == nil {
		return s.logsUnavailable("GetV2SandboxesSandboxIDLogs"), nil
	}
	if clusterID, _ := cluster.SplitSandboxID(req.SandboxID); s.isCrossCluster(clusterID) {
		s.forwarder.Forward(httpctx.GinFromCtx(ctx), clusterID, service.URLKindE2B, nil)
		return nil, nil
	}

	q := logQuery{limit: defaultLogLimit}
	// v2 names the same "start from this millisecond" parameter `cursor`.
	if req.Params.Cursor != nil {
		q.startMillis = req.Params.Cursor
	}
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		q.limit = int(*req.Params.Limit)
	}
	if req.Params.Direction != nil && *req.Params.Direction == e2bgen.LogsDirectionBackward {
		q.backward = true
	}
	if req.Params.Level != nil {
		q.minLevel = req.Params.Level
	}
	if req.Params.Search != nil {
		q.search = req.Params.Search
	}

	entries, appErr := s.fetchLogEntries(ctx, req.SandboxID, q)
	if appErr != nil {
		if appErr.Code == apidomain.ErrCodeNotFound {
			return e2bgen.GetV2SandboxesSandboxIDLogs404JSONResponse{
				N404JSONResponse: e2bgen.N404JSONResponse(errRespCode(404, appErr.Message))}, nil
		}
		return e2bgen.GetV2SandboxesSandboxIDLogs500JSONResponse{
			N500JSONResponse: e2bgen.N500JSONResponse(errRespAppErr(ctx, appErr))}, nil
	}
	return e2bgen.GetV2SandboxesSandboxIDLogs200JSONResponse{Logs: entries}, nil
}
