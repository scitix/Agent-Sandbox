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

package wsdial

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// serve spins up a test server whose handler never upgrades, so every Dial
// fails and we can assert on how the failure is described.
func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestDialDescribesApplicationRejection(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-AgentBox-Server-Version", "1.2.3")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid sync token"}`))
	})

	_, err := Dial(&websocket.Dialer{}, url, nil)
	if err == nil {
		t.Fatal("expected a handshake error")
	}
	got := err.Error()
	for _, want := range []string{"401", "reached-agentbox=1.2.3", "invalid sync token"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q does not mention %q", got, want)
		}
	}
}

func TestDialFlagsRejectionBeforeAgentBox(t *testing.T) {
	// The real signature of an edge proxy refusing the upgrade: a bare 403 with
	// no AgentBox headers on it.
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "envoy")
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := Dial(&websocket.Dialer{}, url, nil)
	if err == nil {
		t.Fatal("expected a handshake error")
	}
	got := err.Error()
	for _, want := range []string{"403", `server="envoy"`, "did not reach AgentBox"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q does not mention %q", got, want)
		}
	}
	if strings.Contains(got, "reached-agentbox") {
		t.Errorf("error %q should not claim the request reached AgentBox", got)
	}
}

func TestDialCollapsesMultiLineBodyOntoOneLine(t *testing.T) {
	url := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>\n<head><title>404 Not Found</title></head>\n<body>\n</body>\n</html>\n"))
	})

	_, err := Dial(&websocket.Dialer{}, url, nil)
	if err == nil {
		t.Fatal("expected a handshake error")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error must stay on one line, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "404 Not Found") {
		t.Errorf("error %q does not quote the body", err.Error())
	}
}

func TestDialPassesThroughTransportErrors(t *testing.T) {
	// Nothing listening — no response to describe, so the raw error survives.
	_, err := Dial(&websocket.Dialer{}, "ws://127.0.0.1:1/nope", nil)
	if err == nil {
		t.Fatal("expected a dial error")
	}
	if strings.Contains(err.Error(), "HTTP ") {
		t.Errorf("transport error should not be dressed up as an HTTP response: %q", err.Error())
	}
}
