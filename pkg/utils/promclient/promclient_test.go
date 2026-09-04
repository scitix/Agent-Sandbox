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

package promclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveJSON(t *testing.T, body string, check func(*http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestQuery_ParsesVector(t *testing.T) {
	srv := serveJSON(t, `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"pod":"p1"},"value":[1788517856,"0.42"]},
		{"metric":{"pod":"p2"},"value":[1788517856,"1"]}]}}`, nil)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	series, err := c.Query(context.Background(), "up", time.Time{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(series))
	}
	if series[0].Labels["pod"] != "p1" || series[0].Samples[0].Value != 0.42 {
		t.Fatalf("unexpected first series: %+v", series[0])
	}
}

// Prometheus sends the value as a string even though the timestamp is numeric;
// decoding one shape and assuming the other is the classic way to lose data.
func TestParseSample_AcceptsBothWireShapes(t *testing.T) {
	if s, ok := parseSample([]any{float64(1788517856), "1.5"}); !ok || s.Value != 1.5 {
		t.Fatalf("string value not parsed: %+v ok=%v", s, ok)
	}
	if s, ok := parseSample([]any{"1788517856", float64(2)}); !ok || s.Value != 2 {
		t.Fatalf("numeric value not parsed: %+v ok=%v", s, ok)
	}
	// NaN means "no data in this window" — not a sample, and not an error.
	if _, ok := parseSample([]any{float64(1), "NaN"}); ok {
		t.Fatal("NaN must not become a sample")
	}
	if _, ok := parseSample([]any{float64(1)}); ok {
		t.Fatal("a malformed pair must not become a sample")
	}
}

func TestQueryRange_ParsesMatrix(t *testing.T) {
	srv := serveJSON(t, `{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"pod":"p1"},"values":[[1788517800,"1"],[1788517860,"2"]]}]}}`, func(r *http.Request) {
		if r.URL.Query().Get("step") == "" {
			t.Error("range query must send a step")
		}
	})
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	series, err := c.QueryRange(context.Background(), "up", time.Unix(1788517800, 0), time.Unix(1788517860, 0), time.Minute)
	if err != nil {
		t.Fatalf("query_range: %v", err)
	}
	if len(series) != 1 || len(series[0].Samples) != 2 {
		t.Fatalf("unexpected series: %+v", series)
	}
}

func TestQuery_SendsConfiguredHeaders(t *testing.T) {
	var got string
	srv := serveJSON(t, `{"status":"success","data":{"result":[]}}`, func(r *http.Request) {
		got = r.Header.Get("Authorization")
	})
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Headers: map[string]string{"Authorization": "Bearer tok"}})
	if _, err := c.Query(context.Background(), "up", time.Time{}); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != "Bearer tok" {
		t.Fatalf("credential not sent, got %q", got)
	}
}

// The endpoint arrives on the cluster-config stream, so it has to be
// replaceable while the process runs — a rotated credential must not need a
// restart.
func TestSetConfig_TakesEffectWithoutRestart(t *testing.T) {
	c := New(Config{})
	if c.Ready() {
		t.Fatal("an unconfigured client must not report ready")
	}
	if _, err := c.Query(context.Background(), "up", time.Time{}); !errors.Is(err, ErrNotConfigured()) {
		t.Fatalf("expected a not-configured error, got %v", err)
	}

	srv := serveJSON(t, `{"status":"success","data":{"result":[]}}`, nil)
	defer srv.Close()
	c.SetConfig(Config{BaseURL: srv.URL})
	if !c.Ready() {
		t.Fatal("expected ready after configuration")
	}
	if _, err := c.Query(context.Background(), "up", time.Time{}); err != nil {
		t.Fatalf("query after reconfigure: %v", err)
	}
}

func TestQuery_ReportsBackendErrors(t *testing.T) {
	srv := serveJSON(t, `{"status":"error","error":"parse error"}`, nil)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	if _, err := c.Query(context.Background(), "((", time.Time{}); err == nil {
		t.Fatal("expected the backend error to surface")
	}
}
