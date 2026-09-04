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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// role identifies which listener a connection arrived on, controlling how (and
// whether) the destination hostname is peeked.
type role int

const (
	roleHTTP  role = iota // port 80: peek HTTP Host header
	roleTLS               // port 443: peek TLS SNI
	roleOther             // everything else: CIDR-only, no L7 peek
)

const upstreamDialTimeout = 30 * time.Second

// statsInterval is how often the proxy reports its own footprint. Only emitted
// at debug level: it exists so a sidecar that got OOM-killed leaves a trail of
// what it was holding, which a crash alone does not tell you.
const statsInterval = 60 * time.Second

// ServeConfig configures the proxy listeners and policy source.
type ServeConfig struct {
	PolicyPath  string
	SecretsPath string
	HTTPPort    int
	TLSPort     int
	OtherPort   int
	// HealthPort serves a plain-text liveness/readiness endpoint on /healthz.
	// Deliberately separate from the three data-plane ports: a probe aimed at
	// those is indistinguishable from a redirected sandbox connection, so it
	// gets policy-evaluated, logged, and (before the self-dial guard) could be
	// dialed back into this same listener. Zero disables it.
	HealthPort int
	Logger     *slog.Logger
}

// Proxy is the running egress filter.
type Proxy struct {
	cfg     ServeConfig
	log     *slog.Logger
	policy  atomic.Pointer[Policy]
	secrets atomic.Pointer[Secrets]
	ca      atomic.Pointer[certAuthority]
	conns   atomic.Int64
}

// NewProxy builds a Proxy, loading the initial policy (fail-closed if absent)
// and injection config (empty if absent).
func NewProxy(cfg ServeConfig) *Proxy {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	p := &Proxy{cfg: cfg, log: cfg.Logger}
	pol, err := LoadPolicy(cfg.PolicyPath)
	if err != nil {
		p.log.Error("initial policy load failed; failing closed", "err", err)
	}
	p.policy.Store(&pol)
	p.loadSecrets()
	return p
}

func (p *Proxy) current() Policy { return *p.policy.Load() }

func (p *Proxy) currentSecrets() Secrets {
	if s := p.secrets.Load(); s != nil {
		return *s
	}
	return Secrets{}
}

func (p *Proxy) currentCA() *certAuthority { return p.ca.Load() }

// loadSecrets re-reads the injection config and rebuilds the CA. A parse
// failure clears both rather than keeping stale material: an operator that
// pushed a bad config must not leave the previous sandbox's credentials armed.
func (p *Proxy) loadSecrets() {
	s, err := LoadSecrets(p.cfg.SecretsPath)
	if err != nil {
		p.log.Error("secrets load failed; injection disabled", "err", err)
		s = Secrets{}
	}
	p.secrets.Store(&s)

	if s.CACertPEM == "" || s.CAKeyPEM == "" {
		p.ca.Store(nil)
	} else {
		ca, caErr := newCertAuthority(s.CACertPEM, s.CAKeyPEM)
		if caErr != nil {
			p.log.Error("secrets: CA material unusable; TLS interception disabled", "err", caErr)
			p.ca.Store(nil)
		} else {
			p.ca.Store(ca)
		}
	}
	// Deliberately logs counts only — never a header value or key.
	p.log.Info("injection config loaded", "sandbox", s.SandboxID,
		"rules", len(s.Rules), "ca", p.ca.Load() != nil)
}

// Serve starts the three listeners and the policy hot-reload watcher, blocking
// until ctx is cancelled.
func (p *Proxy) Serve(ctx context.Context) error {
	go p.watchPolicy(ctx)
	go p.reportStats(ctx)
	if p.cfg.HealthPort > 0 {
		if err := p.serveHealth(ctx); err != nil {
			return err
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	for _, l := range []struct {
		port int
		r    role
	}{
		{p.cfg.HTTPPort, roleHTTP},
		{p.cfg.TLSPort, roleTLS},
		{p.cfg.OtherPort, roleOther},
	} {
		ln, err := net.Listen("tcp", ":"+strconv.Itoa(l.port))
		if err != nil {
			return fmt.Errorf("listen :%d: %w", l.port, err)
		}
		p.log.Info("egress listener up", "port", l.port, "role", l.r)
		wg.Add(1)
		go func(ln net.Listener) {
			defer wg.Done()
			<-ctx.Done()
			_ = ln.Close()
		}(ln)
		wg.Add(1)
		go func(ln net.Listener, r role) {
			defer wg.Done()
			for {
				conn, err := ln.Accept()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					errCh <- err
					return
				}
				go p.handle(ctx, conn, r)
			}
		}(ln, l.r)
	}

	select {
	case <-ctx.Done():
		wg.Wait()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// serveHealth starts the health endpoint. It answers 200 on /healthz as soon as
// the listener is up, which is exactly the signal a readiness probe needs: the
// proxy accepts connections, so the redirect it sits behind will not refuse the
// sandbox's traffic.
func (p *Proxy) serveHealth(ctx context.Context) error {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(p.cfg.HealthPort))
	if err != nil {
		return fmt.Errorf("listen health :%d: %w", p.cfg.HealthPort, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.log.Error("health endpoint stopped", "err", err)
		}
	}()
	p.log.Info("egress health endpoint up", "port", p.cfg.HealthPort)
	return nil
}

// reportStats periodically logs the proxy's own footprint at debug level. A
// sidecar that dies of OOM leaves no explanation behind; this is the trail.
func (p *Proxy) reportStats(ctx context.Context) {
	t := time.NewTicker(statsInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			p.log.Debug("egress proxy stats",
				"conns", p.conns.Load(),
				"goroutines", runtime.NumGoroutine(),
				"heapAllocBytes", m.HeapAlloc,
				"sysBytes", m.Sys,
				"gomaxprocs", runtime.GOMAXPROCS(0))
		}
	}
}

// isSelfDial reports whether the connection's original destination is this very
// listener, which would make forwarding it a loop back into ourselves.
func isSelfDial(conn net.Conn, origIP net.IP, origPort int) bool {
	local, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok || origIP == nil {
		return false
	}
	return local.Port == origPort && local.IP.Equal(origIP)
}

func (p *Proxy) watchPolicy(ctx context.Context) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		p.log.Error("fsnotify unavailable; policy will not hot-reload", "err", err)
		return
	}
	defer func() { _ = w.Close() }()
	// Watch the directory: the control plane writes via temp+rename, so the
	// file inode changes and a direct file watch would go stale.
	dir := dirOf(p.cfg.PolicyPath)
	if err := w.Add(dir); err != nil {
		p.log.Error("watch policy dir failed", "dir", dir, "err", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-w.Events:
			// Both files are written temp+rename into this directory, so match
			// on the directory and re-read whichever one the event names.
			if dirOf(ev.Name) != dir && ev.Name != p.cfg.PolicyPath && ev.Name != p.cfg.SecretsPath {
				continue
			}
			if ev.Name == p.cfg.SecretsPath {
				p.loadSecrets()
				continue
			}
			p.reload()
			if p.cfg.SecretsPath != "" {
				// A rename lands as an event on the temp name, so a change to
				// the secrets file can arrive without naming it directly.
				p.loadSecrets()
			}
		case err := <-w.Errors:
			p.log.Error("fsnotify error", "err", err)
		}
	}
}

func (p *Proxy) reload() {
	pol, err := LoadPolicy(p.cfg.PolicyPath)
	if err != nil {
		p.log.Error("policy reload failed; failing closed", "err", err)
	}
	p.policy.Store(&pol)
	p.log.Info("policy reloaded", "sandbox", pol.SandboxID, "enforce", pol.Enforce,
		"disableEgress", pol.DisableEgress, "domains", len(pol.AllowedDomains), "allowCIDR", len(pol.AllowedCIDRs))
}

func (p *Proxy) handle(ctx context.Context, conn net.Conn, r role) {
	defer func() { _ = conn.Close() }()

	origIP, origPort, err := originalDst(conn)
	if err != nil {
		p.log.Warn("SO_ORIGINAL_DST failed; dropping", "err", err)
		return
	}

	// Refuse to dial ourselves. A connection whose original destination is the
	// very address it arrived on is either a health check aimed at the data-plane
	// port or a sandbox aiming at the Pod's own IP; dialing it would hand the
	// bytes back to this listener, whose handler would dial again, and the chain
	// grows until the sidecar is out of memory. Nothing legitimate needs it — the
	// redirect exempts the proxy's own uid, so a real redirected connection never
	// carries our own listen address as its destination.
	if isSelfDial(conn, origIP, origPort) {
		p.log.Warn("refusing to dial own listener; dropping",
			"ip", origIP.String(), "port", origPort)
		return
	}

	p.conns.Add(1)
	defer p.conns.Add(-1)

	br := bufio.NewReader(conn)
	var hostname string
	switch r {
	case roleTLS:
		hostname = peekTLSSNI(br)
	case roleHTTP:
		hostname = peekHTTPHost(br)
	case roleOther:
		hostname = ""
	}

	decision := p.current().Evaluate(hostname, origIP)
	if !decision.Allow {
		p.log.Info("egress denied", "host", hostname, "ip", origIP.String(), "port", origPort, "match", decision.Match)
		return
	}

	// Credential injection: only for hosts that have rules, and only on the two
	// ports where the destination hostname is knowable. Everything else falls
	// through to the untouched splice path below.
	if r != roleOther {
		if secrets := p.currentSecrets(); secrets.Intercepts(hostname, origPort) {
			p.serveL7(ctx, conn, br, hostname, origPort, r)
			return
		}
	}

	var upstream net.Conn
	if decision.Match == MatchDomain {
		// Dial by hostname (not the sandbox-resolved IP) to defeat /etc/hosts
		// spoofing, and verify the resolved IP is not internal before connect.
		upstream, err = p.dialHostname(ctx, hostname, origPort)
	} else {
		upstream, err = net.DialTimeout("tcp", net.JoinHostPort(origIP.String(), strconv.Itoa(origPort)), upstreamDialTimeout)
	}
	if err != nil {
		p.log.Warn("upstream dial failed", "host", hostname, "ip", origIP.String(), "port", origPort, "err", err)
		return
	}
	defer func() { _ = upstream.Close() }()

	splice(conn, br, upstream)
}

// dialHostname resolves and connects to host:port, rejecting (before connect)
// any candidate IP inside the anti-SSRF denied ranges — blocking DNS-rebind to
// internal targets. Honors AllowPrivateNetworks.
func (p *Proxy) dialHostname(ctx context.Context, host string, port int) (net.Conn, error) {
	allowPrivate := p.current().AllowPrivateNetworks
	d := &net.Dialer{
		Timeout: upstreamDialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			h, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(h)
			if ip == nil {
				return fmt.Errorf("unparseable resolved address %q", address)
			}
			if !allowPrivate && isPrivateIP(ip) {
				return fmt.Errorf("resolved to internal IP %s (blocked)", ip)
			}
			return nil
		},
	}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

// splice copies bidirectionally, seeding the client→upstream direction from the
// bufio.Reader so the peeked bytes are forwarded intact.
func splice(client net.Conn, clientBuf *bufio.Reader, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, clientBuf)
		halfClose(upstream)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		halfClose(client)
		done <- struct{}{}
	}()
	<-done
	<-done
}

func halfClose(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

// originalDst recovers the pre-REDIRECT destination via SO_ORIGINAL_DST.
func originalDst(conn net.Conn) (net.IP, int, error) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, 0, errors.New("not a TCP connection")
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return nil, 0, err
	}
	var ip net.IP
	var port int
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ip, port, sockErr = getOrigDst(int(fd))
	}); err != nil {
		return nil, 0, err
	}
	if sockErr != nil {
		return nil, 0, sockErr
	}
	return ip, port, nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}
