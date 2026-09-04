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

// Package logclient reads container logs from the platform's central log
// service.
//
// A sandbox's Pod holds its logs only while it exists. Once the sandbox is
// released the Pod is recycled and the Kubernetes log API has nothing left to
// return — but the lines were shipped to the central service, which is where
// anyone asking "what did that run print" has to be sent. The dashboard has
// queried it since the log integration landed; this is the same query from the
// worker, so the API can answer for a sandbox that has already ended.
package logclient

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Entry is one log line as the service returns it.
type Entry struct {
	Timestamp     time.Time
	Log           string
	ContainerName string
	PodName       string
}

// Config addresses the central log service.
type Config struct {
	// URL is the download endpoint, e.g.
	// https://op.example.com/api/v1/rms/logging/download
	URL string
	// AppID selects the authentication scheme: set it for the signed scheme
	// (Signature/AppID/Timestamp/Randstr), leave it empty for Bearer.
	AppID string
	// Token is the signing token, or the bearer token when AppID is empty.
	Token string
	// Project scopes the query on gateways that require it; empty omits it.
	Project string
	// Timeout bounds one query.
	Timeout time.Duration
}

// Client queries the central log service.
type Client struct {
	mu   sync.RWMutex
	cfg  Config
	http *http.Client
}

// New returns a Client. An empty URL or Token leaves it unconfigured; callers
// must check Ready.
func New(cfg Config) *Client {
	c := &Client{http: &http.Client{Timeout: 30 * time.Second}}
	c.SetConfig(cfg)
	return c
}

// SetConfig replaces the endpoint and credentials.
func (c *Client) SetConfig(cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
	c.cfg.URL = strings.TrimSpace(cfg.URL)
	c.cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.Timeout > 0 {
		c.http.Timeout = cfg.Timeout
	}
}

// Ready reports whether the service is configured.
func (c *Client) Ready() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg.URL != "" && c.cfg.Token != ""
}

// Query returns the lines the named Pod produced within [start, end].
//
// filters scope the query to one cluster (region / cluster labels). They are
// not optional in practice: pod names are unique per cluster, not globally, so
// an unscoped query can return another cluster's output for the same name.
func (c *Client) Query(
	ctx context.Context,
	podName string,
	filters map[string]string,
	start, end time.Time,
	limit int,
) ([]Entry, error) {
	c.mu.RLock()
	cfg := c.cfg
	c.mu.RUnlock()
	if cfg.URL == "" || cfg.Token == "" {
		return nil, fmt.Errorf("no central log service configured")
	}

	body := map[string]any{
		"kind":       "container_stdout",
		"filters":    buildFilters(podName, filters),
		"start_time": start.UnixMilli(),
		"end_time":   end.UnixMilli(),
		"sort_order": "asc",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := cfg.URL
	if cfg.Project != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + "project=" + cfg.Project
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	hdrs, err := authHeaders(cfg)
	if err != nil {
		return nil, err
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("log service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return decodeNDJSON(resp.Body, limit), nil
}

// buildFilters wraps every value in the service's equality-matcher shape.
func buildFilters(podName string, extra map[string]string) map[string]any {
	out := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		out[k] = map[string]string{"op": "eq", "value": v}
	}
	out["pod_name"] = map[string]string{"op": "eq", "value": podName}
	return out
}

// authHeaders builds either the signed or the bearer envelope. Which one is in
// use is decided by whether an AppID was configured; the request body is
// identical either way.
func authHeaders(cfg Config) (map[string]string, error) {
	if cfg.AppID == "" {
		return map[string]string{
			"Authorization": "Bearer " + cfg.Token,
			"Content-Type":  "application/json",
		}, nil
	}
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(buf)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256([]byte(cfg.Token + nonce + ts))
	return map[string]string{
		"Signature":    hex.EncodeToString(sum[:]),
		"AppID":        cfg.AppID,
		"Timestamp":    ts,
		"Randstr":      nonce,
		"Content-Type": "application/json",
	}, nil
}

// ndjsonLine is the service's wire shape for one entry.
type ndjsonLine struct {
	Timestamp     string `json:"_timestamp"`
	Log           string `json:"log"`
	ContainerName string `json:"container_name"`
	PodName       string `json:"pod_name"`
	// Meta marks the stream's terminal line rather than a log entry.
	Meta bool `json:"_meta"`
}

// decodeNDJSON reads the streamed response, stopping at limit.
//
// It never fails: a malformed line is skipped and a truncated stream returns
// what was read. The caller asked for logs, and partial output beats an error
// that discards lines already in hand.
func decodeNDJSON(r io.Reader, limit int) []Entry {
	sc := bufio.NewScanner(r)
	// Log lines can be long; the default 64 KiB token limit would turn one into
	// a scan error and lose the rest of the stream with it.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var out []Entry
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var l ndjsonLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			// One malformed line is not a reason to discard the rest.
			continue
		}
		if l.Meta {
			continue
		}
		e := Entry{Log: l.Log, ContainerName: l.ContainerName, PodName: l.PodName}
		if l.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
				e.Timestamp = ts
			}
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	// sc.Err() is deliberately ignored: a truncated stream still yields the
	// lines already parsed, which is more useful than none.
	return out
}

// CloneFilters returns a copy safe to hand to a query.
func CloneFilters(in map[string]string) map[string]string {
	return maps.Clone(in)
}
