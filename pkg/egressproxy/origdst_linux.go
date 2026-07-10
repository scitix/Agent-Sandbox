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

//go:build linux

package egressproxy

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// soOriginalDst is the getsockopt option that returns the pre-DNAT/REDIRECT
// destination of a connection (from netfilter conntrack).
const soOriginalDst = 80

// getOrigDst reads SO_ORIGINAL_DST off a redirected socket. GetsockoptIPv6Mreq
// is a well-worn hack: its 16-byte Multiaddr field is wide enough to hold the
// returned sockaddr_in (family, port, addr), letting us avoid a raw syscall.
func getOrigDst(fd int) (net.IP, int, error) {
	// IPv4 first.
	mreq, err := unix.GetsockoptIPv6Mreq(fd, unix.IPPROTO_IP, soOriginalDst)
	if err == nil {
		b := mreq.Multiaddr // [16]byte: family(2) port(2,BE) addr(4) ...
		port := int(b[2])<<8 | int(b[3])
		ip := net.IPv4(b[4], b[5], b[6], b[7])
		return ip, port, nil
	}
	// IPv6 fallback (IP6T_SO_ORIGINAL_DST shares the option number).
	sa, err6 := unix.GetsockoptIPv6MTUInfo(fd, unix.IPPROTO_IPV6, soOriginalDst)
	if err6 == nil {
		addr := sa.Addr
		ip := make(net.IP, net.IPv6len)
		copy(ip, addr.Addr[:])
		port := int(addr.Port>>8) | int(addr.Port<<8)&0xffff // ntohs
		return ip, port, nil
	}
	return nil, 0, fmt.Errorf("getsockopt SO_ORIGINAL_DST: v4=%v v6=%v", err, err6)
}
