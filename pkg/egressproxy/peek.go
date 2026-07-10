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
	"strings"
)

// peekTLSSNI extracts the SNI server name from a TLS ClientHello by peeking
// (not consuming) the buffered stream, so the full record can still be spliced
// to the upstream. Returns "" if the bytes are not a ClientHello or carry no
// SNI. Bounds are checked defensively — a malformed record yields "".
func peekTLSSNI(r *bufio.Reader) string {
	// TLS record header: type(1)=22 handshake, version(2), length(2).
	hdr, err := r.Peek(5)
	if err != nil || hdr[0] != 0x16 {
		return ""
	}
	recLen := int(hdr[3])<<8 | int(hdr[4])
	if recLen < 4 {
		return ""
	}
	buf, err := r.Peek(5 + recLen)
	if err != nil {
		// Record may span multiple TCP segments; peek as much as buffered.
		buf, _ = r.Peek(r.Buffered())
		if len(buf) < 5 {
			return ""
		}
	}
	return parseClientHelloSNI(buf[5:])
}

// parseClientHelloSNI walks a TLS handshake ClientHello body and returns the
// first server_name (type host_name) from the SNI extension.
func parseClientHelloSNI(b []byte) string {
	// Handshake header: type(1)=1 client_hello, length(3).
	if len(b) < 4 || b[0] != 0x01 {
		return ""
	}
	b = b[4:] // skip handshake header; remaining is ClientHello body
	// client_version(2) + random(32)
	if len(b) < 34 {
		return ""
	}
	b = b[34:]
	// session_id
	if len(b) < 1 {
		return ""
	}
	sidLen := int(b[0])
	b = b[1:]
	if len(b) < sidLen {
		return ""
	}
	b = b[sidLen:]
	// cipher_suites
	if len(b) < 2 {
		return ""
	}
	csLen := int(b[0])<<8 | int(b[1])
	b = b[2:]
	if len(b) < csLen {
		return ""
	}
	b = b[csLen:]
	// compression_methods
	if len(b) < 1 {
		return ""
	}
	cmLen := int(b[0])
	b = b[1:]
	if len(b) < cmLen {
		return ""
	}
	b = b[cmLen:]
	// extensions
	if len(b) < 2 {
		return ""
	}
	extTotal := int(b[0])<<8 | int(b[1])
	b = b[2:]
	if len(b) > extTotal {
		b = b[:extTotal]
	}
	for len(b) >= 4 {
		extType := int(b[0])<<8 | int(b[1])
		extLen := int(b[2])<<8 | int(b[3])
		b = b[4:]
		if len(b) < extLen {
			return ""
		}
		if extType == 0x0000 { // server_name
			return parseSNIExtension(b[:extLen])
		}
		b = b[extLen:]
	}
	return ""
}

func parseSNIExtension(b []byte) string {
	// ServerNameList: list_length(2), then entries: type(1), name_length(2), name.
	if len(b) < 2 {
		return ""
	}
	listLen := int(b[0])<<8 | int(b[1])
	b = b[2:]
	if len(b) > listLen {
		b = b[:listLen]
	}
	for len(b) >= 3 {
		nameType := b[0]
		nameLen := int(b[1])<<8 | int(b[2])
		b = b[3:]
		if len(b) < nameLen {
			return ""
		}
		if nameType == 0x00 { // host_name
			return string(b[:nameLen])
		}
		b = b[nameLen:]
	}
	return ""
}

// peekHTTPHost reads the request line + headers by peeking and returns the Host
// header value (without port). Returns "" if not a plausible HTTP request or no
// Host is found within the buffered prefix.
func peekHTTPHost(r *bufio.Reader) string {
	// Peek a bounded prefix; headers we care about are near the front.
	const maxPeek = 4096
	n := r.Buffered()
	if n < maxPeek {
		// Trigger a read so a small request's headers are buffered.
		if _, err := r.Peek(min(maxPeek, n+1)); err != nil && r.Buffered() == 0 {
			return ""
		}
		n = r.Buffered()
	}
	if n > maxPeek {
		n = maxPeek
	}
	buf, _ := r.Peek(n)
	// Require a request-line-ish first token to avoid mis-peeking binary.
	if !looksLikeHTTP(buf) {
		return ""
	}
	for line := range strings.SplitSeq(string(buf), "\r\n") {
		if line == "" {
			break // end of headers
		}
		if h, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(h), "host") {
			host := strings.TrimSpace(v)
			if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
				host = host[:i]
			}
			return host
		}
	}
	return ""
}

func looksLikeHTTP(b []byte) bool {
	for _, m := range []string{"GET ", "POST ", "PUT ", "HEAD ", "DELETE ", "PATCH ", "OPTIONS ", "CONNECT ", "TRACE "} {
		if strings.HasPrefix(string(b), m) {
			return true
		}
	}
	return false
}
