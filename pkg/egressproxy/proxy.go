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

// ServeConfig configures the proxy listeners and policy source.
type ServeConfig struct {
	PolicyPath string
	HTTPPort   int
	TLSPort    int
	OtherPort  int
	Logger     *slog.Logger
}

// Proxy is the running egress filter.
type Proxy struct {
	cfg    ServeConfig
	log    *slog.Logger
	policy atomic.Pointer[Policy]
}

// NewProxy builds a Proxy, loading the initial policy (fail-closed if absent).
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
	return p
}

func (p *Proxy) current() Policy { return *p.policy.Load() }

// Serve starts the three listeners and the policy hot-reload watcher, blocking
// until ctx is cancelled.
func (p *Proxy) Serve(ctx context.Context) error {
	go p.watchPolicy(ctx)

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
			if ev.Name == p.cfg.PolicyPath || dirOf(ev.Name) == dir {
				p.reload()
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
