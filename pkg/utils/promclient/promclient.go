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

// Package promclient is a small read-only client for a Prometheus-compatible
// query API.
//
// It exists so the hub's notification queries and the worker's sandbox-metrics
// endpoint share one implementation. Two copies of "parse a Prometheus result
// vector" would drift, and the second copy is where the subtle bugs live —
// a value arriving as a string rather than a number, a partial response, an
// empty vector that means "no data" rather than "zero".
package promclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is one point of a result series.
type Sample struct {
	At    time.Time
	Value float64
}

// Series is one labelled result series.
type Series struct {
	Labels  map[string]string
	Samples []Sample
}

// Config addresses a Prometheus-compatible query API.
type Config struct {
	// BaseURL is the query root, e.g. https://obs.example.com/api/query/metrics
	// (the /api/v1/... paths are appended to it).
	BaseURL string
	// Headers are sent with every request; this is where an Authorization
	// bearer belongs.
	Headers map[string]string
	// Timeout bounds a single query. Zero uses a sane default.
	Timeout time.Duration
}

// Client queries a Prometheus-compatible API.
//
// The configuration is swappable at runtime because it arrives on the
// cluster-config stream: the endpoint and its credential are pushed from the
// hub rather than baked into a flag, so a rotation reaches every worker without
// a restart.
type Client struct {
	mu   sync.RWMutex
	cfg  Config
	http *http.Client
}

// New returns a Client. An empty BaseURL leaves it unconfigured, which callers
// must check with Ready before querying.
func New(cfg Config) *Client {
	c := &Client{http: &http.Client{Timeout: 15 * time.Second}}
	c.SetConfig(cfg)
	return c
}

// SetConfig replaces the endpoint and credentials. Safe to call concurrently
// with queries.
func (c *Client) SetConfig(cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = Config{
		BaseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		Headers: maps.Clone(cfg.Headers),
		Timeout: cfg.Timeout,
	}
	if cfg.Timeout > 0 {
		c.http.Timeout = cfg.Timeout
	}
}

// Ready reports whether an endpoint has been configured.
func (c *Client) Ready() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg.BaseURL != ""
}

func (c *Client) snapshot() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Config{BaseURL: c.cfg.BaseURL, Headers: maps.Clone(c.cfg.Headers)}
}

var errNotConfigured = fmt.Errorf("no metrics endpoint configured")

// ErrNotConfigured reports that no endpoint has been set.
func ErrNotConfigured() error { return errNotConfigured }

func (c *Client) doGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	cfg := c.snapshot()
	if cfg.BaseURL == "" {
		return nil, errNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	// Cap the read: a query that accidentally matches everything should fail
	// this request, not the process.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics backend returned %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	return body, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// queryResponse covers both the instant and range shapes; only one of Value /
// Values is populated per result depending on the query.
type queryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// Query runs an instant query and returns every series in the result vector.
func (c *Client) Query(ctx context.Context, promql string, at time.Time) ([]Series, error) {
	params := url.Values{}
	params.Set("query", promql)
	if !at.IsZero() {
		params.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	body, err := c.doGet(ctx, "/api/v1/query", params)
	if err != nil {
		return nil, err
	}
	return decodeSeries(body)
}

// QueryRange runs a range query over [start, end] at the given step.
func (c *Client) QueryRange(ctx context.Context, promql string, start, end time.Time, step time.Duration) ([]Series, error) {
	params := url.Values{}
	params.Set("query", promql)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.FormatInt(int64(step.Seconds()), 10)+"s")
	body, err := c.doGet(ctx, "/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}
	return decodeSeries(body)
}

func decodeSeries(body []byte) ([]Series, error) {
	var payload queryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode metrics response: %w", err)
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("metrics query failed: %s", payload.Error)
	}

	out := make([]Series, 0, len(payload.Data.Result))
	for _, r := range payload.Data.Result {
		s := Series{Labels: r.Metric}
		if len(r.Value) >= 2 {
			if sample, ok := parseSample(r.Value); ok {
				s.Samples = append(s.Samples, sample)
			}
		}
		for _, v := range r.Values {
			if sample, ok := parseSample(v); ok {
				s.Samples = append(s.Samples, sample)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// parseSample reads Prometheus's [timestamp, "value"] pair. The value is a
// string in the wire format even though the timestamp is a number, so it is
// decoded from either shape rather than assuming one.
func parseSample(pair []any) (Sample, bool) {
	if len(pair) < 2 {
		return Sample{}, false
	}
	var at time.Time
	switch t := pair[0].(type) {
	case float64:
		sec, frac := int64(t), t-float64(int64(t))
		at = time.Unix(sec, int64(frac*1e9)).UTC()
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return Sample{}, false
		}
		at = time.Unix(int64(f), 0).UTC()
	default:
		return Sample{}, false
	}

	switch v := pair[1].(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			// ParseFloat happily accepts "NaN" and "+Inf". Prometheus uses NaN
			// for "no data in this window", which is not an error but is also
			// not a value: passing it on would turn an absent sample into a
			// zero, or into a NaN that poisons whatever averages it.
			return Sample{}, false
		}
		return Sample{At: at, Value: f}, true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Sample{}, false
		}
		return Sample{At: at, Value: v}, true
	default:
		return Sample{}, false
	}
}
