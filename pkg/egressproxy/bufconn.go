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
	"net"
)

// bufConn re-attaches an already-buffered reader to its connection.
//
// The SNI/Host peek reads ahead into a bufio.Reader without consuming from the
// socket's perspective, which is exactly right for the splice path. The MITM
// path instead needs to hand the connection to crypto/tls, which reads from the
// net.Conn directly and would miss the bytes sitting in that buffer. Wrapping
// restores a single ordered stream: buffered bytes first, socket after.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func newBufConn(c net.Conn, r *bufio.Reader) *bufConn {
	return &bufConn{Conn: c, r: r}
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }
