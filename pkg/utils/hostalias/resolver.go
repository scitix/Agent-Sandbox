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

// Package hostalias provides an in-process /etc/hosts-style resolver driven by
// the Manager-pushed ClusterConfig.HostAliases list.
//
// Design rationale:
//
//	Worker pods cannot mutate their own Pod spec, so hostAliases delivered at
//	install time via YAML would require a Deployment-level rollout whenever a
//	remote endpoint changes. Instead the Manager broadcasts host-alias entries
//	through the existing sync channel; each Worker process consumes them in
//	memory and overrides DNS lookups for the owning Go/gRPC components
//	(ExtProc DNS cache, CrossClusterForwarder HTTP client).
//
// The Resolver is intentionally tiny: a hostname → IP lookup table plus a
// net.Dialer DialContext hook. It does not fall back to the system resolver
// itself — callers decide whether an override miss should trigger net.Lookup
// or some other behaviour (e.g. ExtProc's dnsCache currently falls back).
package hostalias

import (
	"context"
	"maps"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"

	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// Resolver keeps a read-optimised host → IP map that is swapped atomically on
// every Manager push. It is safe for concurrent use.
type Resolver struct {
	// lookup holds map[string]string (hostname → IPv4/IPv6 literal).
	// Stored via atomic.Value for lock-free reads on the hot path.
	lookup atomic.Pointer[map[string]string]
}

// New returns an empty Resolver. Attach it to the cluster.Store via Bind so it
// picks up subsequent snapshots automatically.
func New() *Resolver {
	r := &Resolver{}
	empty := map[string]string{}
	r.lookup.Store(&empty)
	return r
}

// Bind subscribes r to store's host-alias updates. Safe to call once at
// startup; the subscription lives for the duration of the process.
func (r *Resolver) Bind(store *cluster.Store) {
	if store == nil {
		return
	}
	store.SubscribeHostAliases(r.Set)
}

// Set replaces the current alias table with the given list. Later entries
// win on duplicate hostnames so that a single IP-change push is
// deterministic. Hostnames are matched case-insensitively.
func (r *Resolver) Set(aliases []corev1.HostAlias) {
	m := make(map[string]string, len(aliases)*2)
	for _, a := range aliases {
		if a.IP == "" {
			continue
		}
		for _, h := range a.Hostnames {
			if h == "" {
				continue
			}
			m[strings.ToLower(h)] = a.IP
		}
	}
	r.lookup.Store(&m)
}

// Lookup returns the override IP for host and true when an override exists.
// The second return value is false when there is no override; callers fall
// back to whatever resolver makes sense for them.
func (r *Resolver) Lookup(host string) (string, bool) {
	m := r.lookup.Load()
	if m == nil {
		return "", false
	}
	ip, ok := (*m)[strings.ToLower(host)]
	return ip, ok
}

// Snapshot returns a copy of the current override map for diagnostics/testing.
func (r *Resolver) Snapshot() map[string]string {
	m := r.lookup.Load()
	if m == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(*m))
	maps.Copy(out, *m)
	return out
}

// DialContext returns a DialContext function suitable for http.Transport or
// any net.Dialer-consuming client. It substitutes the IP for known hostnames
// before handing off to the provided base dialer (nil uses a default
// net.Dialer with standard behaviour).
//
// The substitution preserves the original port, so callers don't need to know
// which scheme (http/https) is in use.
func (r *Resolver) DialContext(base *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if base == nil {
		base = &net.Dialer{}
	}
	dialerMu := sync.Mutex{}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, splitErr := net.SplitHostPort(addr)
		if splitErr == nil {
			if ip, ok := r.Lookup(host); ok {
				addr = net.JoinHostPort(ip, port)
			}
		}
		dialerMu.Lock()
		d := *base
		dialerMu.Unlock()
		return d.DialContext(ctx, network, addr)
	}
}
