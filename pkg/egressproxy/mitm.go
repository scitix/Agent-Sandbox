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
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// l7IdleTimeout bounds how long an intercepted connection may sit idle between
// requests before the proxy reclaims it.
const l7IdleTimeout = 5 * time.Minute

// serveL7 handles a connection whose destination has injection rules: it
// terminates the sandbox side, rewrites each request's headers, and forwards
// over a freshly originated connection to the real upstream.
//
// This path runs *only* for hosts named in the injection table. Everything else
// keeps the byte-for-byte splice, so enabling credential injection cannot
// change the behaviour — or the TLS fingerprint — of unrelated traffic.
func (p *Proxy) serveL7(ctx context.Context, client net.Conn, br *bufio.Reader, hostname string, port int, r role, allowsPrivate bool) {
	secrets := p.currentSecrets()

	clientStream := net.Conn(newBufConn(client, br))
	plaintext := r == roleHTTP

	if !plaintext {
		ca := p.currentCA()
		if ca == nil {
			// Rules exist but no CA was pushed: we cannot impersonate the host.
			// Closing is the honest outcome — falling back to a splice would
			// silently forward the sandbox's un-injected request and look like
			// success while the credential was never added.
			p.log.Error("egress inject: TLS interception requested but no CA is loaded", "host", hostname)
			return
		}
		tlsConn := tls.Server(clientStream, &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Offer only HTTP/1.1 so the intercepted stream is one the proxy can
			// parse. Clients that would have negotiated h2 fall back cleanly.
			NextProtos: []string{"http/1.1"},
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				name := hello.ServerName
				if name == "" {
					name = hostname
				}
				return ca.leafFor(name)
			},
		})
		hsCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := tlsConn.HandshakeContext(hsCtx)
		cancel()
		if err != nil {
			// Almost always the sandbox not trusting our CA (certificate
			// pinning, or a client reading a bundle we did not populate).
			p.log.Error("egress inject: sandbox-side TLS handshake failed", "host", hostname, "err", err)
			return
		}
		defer func() { _ = tlsConn.Close() }()
		clientStream = tlsConn
	}

	upstream, err := p.dialUpstreamFor(ctx, hostname, port, plaintext, allowsPrivate)
	if err != nil {
		p.log.Error("egress inject: upstream dial failed", "host", hostname, "port", port, "err", err)
		return
	}
	defer func() { _ = upstream.Close() }()

	p.pumpRequests(clientStream, upstream, &secrets, hostname, port)
}

// dialUpstreamFor opens the real connection. It reuses dialHostname so the
// anti-rebind guarantee is identical to the splice path: resolve the name
// ourselves, refuse before connect any address the admitting decision would not
// have permitted. For TLS destinations the proxy then performs a *fully
// verified* handshake — interception is applied to the sandbox side only, never
// to the upstream side.
func (p *Proxy) dialUpstreamFor(ctx context.Context, hostname string, port int, plaintext, allowsPrivate bool) (net.Conn, error) {
	raw, err := p.dialHostname(ctx, hostname, port, allowsPrivate)
	if err != nil {
		return nil, err
	}
	if plaintext {
		return raw, nil
	}
	tlsConn := tls.Client(raw, &tls.Config{
		ServerName: hostname,
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	})
	hsCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(hsCtx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tlsConn, nil
}

// pumpRequests reads requests off the intercepted stream, injects, forwards,
// and relays responses until either side closes.
func (p *Proxy) pumpRequests(clientStream, upstream net.Conn, secrets *Secrets, hostname string, port int) {
	creader := bufio.NewReader(clientStream)
	ureader := bufio.NewReader(upstream)

	for served := 0; ; served++ {
		_ = clientStream.SetReadDeadline(time.Now().Add(l7IdleTimeout))
		req, err := http.ReadRequest(creader)
		if err != nil {
			if served == 0 && !errors.Is(err, io.EOF) {
				// Something other than HTTP is speaking on this port. We already
				// terminated TLS, so the bytes cannot be handed back to a
				// splice; report it rather than fail silently.
				p.log.Error("egress inject: destination has injection rules but the stream is not HTTP",
					"host", hostname, "port", port, "err", err)
			}
			return
		}
		_ = clientStream.SetReadDeadline(time.Time{})

		rules := secrets.MatchAll(hostname, port)
		outcome := secrets.Apply(req, rules)
		p.log.Info("egress inject", "host", hostname, "port", port,
			"method", req.Method, "path", req.URL.Path,
			"headersSet", outcome.HeadersSet, "skipped", outcome.Skipped)

		if err := req.Write(upstream); err != nil {
			p.log.Error("egress inject: forwarding request failed", "host", hostname, "err", err)
			return
		}

		resp, err := http.ReadResponse(ureader, req)
		if err != nil {
			p.log.Error("egress inject: reading upstream response failed", "host", hostname, "err", err)
			return
		}

		// Redirects are relayed verbatim, never followed. Following one with the
		// injected header still attached would hand the credential to whatever
		// host the response names.
		if err := resp.Write(clientStream); err != nil {
			_ = resp.Body.Close()
			return
		}
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusSwitchingProtocols {
			// The connection stops being HTTP after a successful upgrade
			// (WebSocket and friends). Headers on the upgrade request were
			// already injected; the rest is an opaque tunnel.
			splice(clientStream, creader, upstream)
			return
		}
		if req.Close || resp.Close {
			return
		}
	}
}
