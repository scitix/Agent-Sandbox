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
	"net"
	"testing"
)

// fakeConn reports a fixed local address; nothing else is exercised.
type fakeConn struct {
	net.Conn
	local net.Addr
}

func (c fakeConn) LocalAddr() net.Addr { return c.local }

// A connection whose original destination is the address it arrived on must be
// dropped. Forwarding it hands the bytes back to this same listener, whose
// handler dials again — the chain grows until the sidecar is OOM-killed, and a
// sandbox can start it deliberately by connecting to its own Pod IP on a
// data-plane port whenever private ranges are allowed.
func TestIsSelfDial(t *testing.T) {
	podIP := net.ParseIP("10.0.0.1")
	conn := fakeConn{local: &net.TCPAddr{IP: podIP, Port: DefaultHTTPPort}}

	if !isSelfDial(conn, podIP, DefaultHTTPPort) {
		t.Error("same IP and port as the listener must be refused")
	}
	// A real redirected connection carries the upstream's address, not ours.
	if isSelfDial(conn, net.ParseIP("93.184.216.34"), 443) {
		t.Error("an ordinary upstream must not be mistaken for a self-dial")
	}
	// Same host, different port: that is a legitimate local service.
	if isSelfDial(conn, podIP, 8080) {
		t.Error("a different port on the same IP is not this listener")
	}
	// Same port on another address: another host's service that happens to run
	// on the same port number.
	if isSelfDial(conn, net.ParseIP("10.0.0.2"), DefaultHTTPPort) {
		t.Error("the same port on a different IP is not this listener")
	}
	if isSelfDial(conn, nil, DefaultHTTPPort) {
		t.Error("a missing destination cannot be a self-dial")
	}
}

// The health port must not collide with the data plane: a probe landing on one
// of those ports is indistinguishable from a redirected sandbox connection.
func TestHealthPortIsSeparateFromDataPlane(t *testing.T) {
	for _, p := range []int{DefaultHTTPPort, DefaultTLSPort, DefaultOtherPort} {
		if DefaultHealthPort == p {
			t.Fatalf("health port %d collides with a data-plane port", DefaultHealthPort)
		}
	}
}
