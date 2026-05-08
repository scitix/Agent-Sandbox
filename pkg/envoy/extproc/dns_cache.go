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

package extproc

import (
	"net"
	"sync"
	"time"

	"github.com/scitix/agent-sandbox/pkg/utils/hostalias"
)

const defaultDNSTTL = 30 * time.Second

// dnsEntry is a single cached DNS result.
type dnsEntry struct {
	ip        string
	expiresAt time.Time
}

// dnsCache is a simple TTL-based in-memory DNS cache.
// It resolves hostnames to their first IP address and caches the result.
//
// Resolution order:
//  1. Manager-pushed host alias overrides (if a resolver is attached).
//     Overrides are authoritative and bypass the TTL cache entirely — they
//     are already in-memory and swapped atomically by hostalias.Resolver, so
//     further caching offers no benefit and would only slow propagation.
//  2. Local TTL cache (30s by default).
//  3. Go's net.LookupHost fallback.
type dnsCache struct {
	mu       sync.Mutex
	entries  map[string]dnsEntry
	ttl      time.Duration
	resolver *hostalias.Resolver // optional; nil disables override lookups
}

func newDNSCache(ttl time.Duration) *dnsCache {
	return &dnsCache{
		entries: make(map[string]dnsEntry),
		ttl:     ttl,
	}
}

// setResolver attaches a host-alias resolver. Calling with nil clears it.
func (c *dnsCache) setResolver(r *hostalias.Resolver) {
	c.mu.Lock()
	c.resolver = r
	c.mu.Unlock()
}

// resolve returns an IP for the given hostname, using the cache when possible.
// On lookup failure it returns an empty string (caller keeps the original hostname).
func (c *dnsCache) resolve(host string) string {
	// Host-alias override takes precedence and is never cached (the resolver
	// itself is already an in-memory map swapped atomically by the Manager).
	c.mu.Lock()
	resolver := c.resolver
	c.mu.Unlock()
	if resolver != nil {
		if ip, ok := resolver.Lookup(host); ok {
			return ip
		}
	}

	now := time.Now()

	c.mu.Lock()
	if e, ok := c.entries[host]; ok && now.Before(e.expiresAt) {
		c.mu.Unlock()
		return e.ip
	}
	c.mu.Unlock()

	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return ""
	}

	c.mu.Lock()
	c.entries[host] = dnsEntry{ip: addrs[0], expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()

	return addrs[0]
}
