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
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGenerateCA_ShapeAndConstraints(t *testing.T) {
	certPEM, keyPEM, err := GenerateCA("agentbox-egress-ca:sbx-1", time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("cert is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cert.IsCA {
		t.Fatal("must be a CA")
	}
	if cert.MaxPathLen != 0 || !cert.MaxPathLenZero {
		t.Fatal("CA must be constrained to signing leaves only")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("CA must be able to sign certificates")
	}
	if got := time.Until(cert.NotAfter); got > 2*time.Hour {
		t.Fatalf("NotAfter is %v away, want the requested short TTL", got)
	}
	if _, err := newCertAuthority(certPEM, keyPEM); err != nil {
		t.Fatalf("the generated pair must load back: %v", err)
	}
}

func TestCertAuthority_LeafIsSignedByCAAndCached(t *testing.T) {
	certPEM, keyPEM, err := GenerateCA("test-ca", time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	ca, err := newCertAuthority(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("newCertAuthority: %v", err)
	}

	leaf, err := ca.leafFor("api.example.com")
	if err != nil {
		t.Fatalf("leafFor: %v", err)
	}
	parsed, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if err := parsed.VerifyHostname("api.example.com"); err != nil {
		t.Fatalf("leaf does not cover the SNI: %v", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(certPEM)) {
		t.Fatal("cannot build root pool")
	}
	if _, err := parsed.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("leaf does not chain to the sandbox CA: %v", err)
	}

	again, err := ca.leafFor("api.example.com")
	if err != nil {
		t.Fatalf("second leafFor: %v", err)
	}
	if again != leaf {
		t.Fatal("repeat lookups must hit the cache, not re-sign")
	}
}

// A leaf may never outlive the sandbox-scoped CA that vouches for it.
func TestCertAuthority_LeafCappedAtCAExpiry(t *testing.T) {
	certPEM, keyPEM, err := GenerateCA("short-ca", 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	ca, err := newCertAuthority(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("newCertAuthority: %v", err)
	}
	leaf, err := ca.leafFor("h.example.com")
	if err != nil {
		t.Fatalf("leafFor: %v", err)
	}
	parsed, _ := x509.ParseCertificate(leaf.Certificate[0])
	if parsed.NotAfter.After(ca.caCert.NotAfter) {
		t.Fatalf("leaf NotAfter %v outlives the CA's %v", parsed.NotAfter, ca.caCert.NotAfter)
	}
}

// pumpRequests is the rewrite engine. Driving it over a plain socket pair
// exercises injection, redirect handling and keep-alive without needing the
// iptables redirect or a TLS handshake.
func runPump(t *testing.T, s Secrets, clientWrite string, upstreamReply func(req *http.Request) string) (upstreamGot *http.Request, clientGot *http.Response) {
	t.Helper()
	p := &Proxy{cfg: ServeConfig{}, log: discardLogger()}

	cA, cB := net.Pipe() // sandbox side
	uA, uB := net.Pipe() // upstream side

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pumpRequests(cB, uA, &s, "api.openai.com", 443)
	}()

	// Fake upstream: read one request, reply.
	upstreamDone := make(chan struct{})
	go func() {
		defer close(upstreamDone)
		br := bufio.NewReader(uB)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		upstreamGot = req
		_, _ = io.WriteString(uB, upstreamReply(req))
	}()

	go func() {
		_, _ = io.WriteString(cA, clientWrite)
	}()

	// net.Pipe is unbuffered and the response arrives in several small writes,
	// so parse it properly rather than grabbing whatever one Read returns.
	_ = cA.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(cA), nil)
	if err != nil {
		t.Fatalf("reading the relayed response: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	clientGot = resp

	_ = cA.Close()
	_ = uB.Close()
	<-upstreamDone
	<-done
	return upstreamGot, clientGot
}

func TestPumpRequests_InjectsHeaderOnTheWayOut(t *testing.T) {
	s := secretsFixture()
	req := "GET /v1/models HTTP/1.1\r\nHost: api.openai.com\r\nConnection: close\r\n\r\n"
	got, _ := runPump(t, s, req, func(*http.Request) string {
		return "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"
	})
	if got == nil {
		t.Fatal("upstream received no request")
	}
	if v := got.Header.Get("Authorization"); v != testRealKey {
		t.Fatalf("upstream saw Authorization=%q, want the injected credential", v)
	}
}

// The sandbox sends only a decoy; the real credential appears on the wire.
func TestPumpRequests_SubstitutesDecoyOnTheWayOut(t *testing.T) {
	s := secretsFixture()
	s.Rules[0].Headers = nil
	req := "GET /v1/models HTTP/1.1\r\nHost: api.openai.com\r\n" +
		"Authorization: Bearer agbx_ph_decoy0000000000\r\nConnection: close\r\n\r\n"
	got, _ := runPump(t, s, req, func(*http.Request) string {
		return "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"
	})
	if got == nil {
		t.Fatal("upstream received no request")
	}
	if v := got.Header.Get("Authorization"); v != testRealKey {
		t.Fatalf("upstream saw Authorization=%q, want the decoy replaced", v)
	}
}

// Redirects are relayed, never followed: following one with the injected header
// still attached would hand the credential to whatever host the response names.
func TestPumpRequests_DoesNotFollowRedirects(t *testing.T) {
	s := secretsFixture()
	req := "GET /v1/models HTTP/1.1\r\nHost: api.openai.com\r\nConnection: close\r\n\r\n"
	_, resp := runPump(t, s, req, func(*http.Request) string {
		return "HTTP/1.1 302 Found\r\nLocation: https://evil.example.com/steal\r\n" +
			"Content-Length: 0\r\nConnection: close\r\n\r\n"
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("client saw status %d, want the 302 relayed verbatim", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "evil.example.com") {
		t.Fatalf("Location=%q, want it relayed for the client to decide on", loc)
	}
	// The proxy must not have chased the redirect itself; had it done so the
	// injected credential would have reached evil.example.com.
}

// Requests outside the rule's path narrowing must leave with nothing added.
func TestPumpRequests_SkippedPathCarriesNoCredential(t *testing.T) {
	s := secretsFixture()
	req := "GET /admin/keys HTTP/1.1\r\nHost: api.openai.com\r\nConnection: close\r\n\r\n"
	got, _ := runPump(t, s, req, func(*http.Request) string {
		return "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
	})
	if got == nil {
		t.Fatal("upstream received no request")
	}
	if v := got.Header.Get("Authorization"); v != "" {
		t.Fatalf("upstream saw Authorization=%q on a path the rule does not cover", v)
	}
}

// discardLogger silences proxy logging in tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
