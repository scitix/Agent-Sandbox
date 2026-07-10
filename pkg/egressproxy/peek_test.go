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

package egressproxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"net"
	"testing"
	"time"
)

// clientHelloBytes captures the raw TLS ClientHello a client sends for a given
// SNI, by driving crypto/tls over an in-memory pipe.
func clientHelloBytes(t *testing.T, server string) []byte {
	t.Helper()
	c1, c2 := net.Pipe()
	go func() {
		cli := tls.Client(c1, &tls.Config{ServerName: server, InsecureSkipVerify: true}) //nolint:gosec // test only
		_ = cli.Handshake()                                                              // blocks awaiting server reply; we only want the first flight
		_ = c1.Close()
	}()
	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := c2.Read(buf)
	_ = c2.Close()
	if err != nil && n == 0 {
		t.Fatalf("capture ClientHello: %v", err)
	}
	return buf[:n]
}

func TestPeekTLSSNI(t *testing.T) {
	hello := clientHelloBytes(t, "files.pythonhosted.org")
	br := bufio.NewReader(bytes.NewReader(hello))
	if got := peekTLSSNI(br); got != "files.pythonhosted.org" {
		t.Errorf("SNI = %q, want files.pythonhosted.org", got)
	}
	// Peeking must not consume: the full record is still readable for splicing.
	rest, _ := br.Peek(len(hello))
	if len(rest) != len(hello) {
		t.Errorf("peek consumed bytes: have %d want %d", len(rest), len(hello))
	}
}

func TestPeekTLSSNI_NotTLS(t *testing.T) {
	br := bufio.NewReader(bytes.NewReader([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")))
	if got := peekTLSSNI(br); got != "" {
		t.Errorf("non-TLS should yield empty SNI, got %q", got)
	}
}

func TestPeekHTTPHost(t *testing.T) {
	cases := []struct{ req, want string }{
		{"GET /simple/ HTTP/1.1\r\nHost: pypi.org\r\nUser-Agent: pip\r\n\r\n", "pypi.org"},
		{"POST / HTTP/1.1\r\nHost: registry.npmjs.org:443\r\n\r\n", "registry.npmjs.org"},
		{"GET / HTTP/1.1\r\nhost: Example.COM\r\n\r\n", "Example.COM"},
		{"\x16\x03\x01 binary tls-ish", ""}, // not HTTP
	}
	for _, c := range cases {
		br := bufio.NewReader(bytes.NewReader([]byte(c.req)))
		if got := peekHTTPHost(br); got != c.want {
			t.Errorf("peekHTTPHost(%q) = %q, want %q", c.req, got, c.want)
		}
	}
}
