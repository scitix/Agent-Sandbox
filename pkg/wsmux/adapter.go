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

// Package wsmux adapts a gorilla/websocket connection into a net.Conn so that
// yamux can multiplex independent logical streams on top of it, and gRPC can
// run on top of yamux. This is the foundation of the Hub ↔ Worker sync
// protocol: a single long-lived WebSocket carries many independent gRPC
// streams (unary RPCs + server-stream Watch subscriptions) without
// head-of-line blocking between them.
//
// Read MUST use websocket.NextReader (not ReadMessage) so that yamux can pull
// bytes incrementally; if we buffered each WebSocket message whole, yamux's
// chunk-level fair-scheduling between streams would degenerate to "one message
// blocks the next".
package wsmux

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsAddr is a placeholder net.Addr used when the underlying connection has no
// meaningful address (e.g. one side of a net.Pipe). gorilla returns nil for
// websocket.Conn.LocalAddr() when running over net.Pipe; net.Conn callers
// (including grpc) expect a non-nil address with String().
type wsAddr struct{ s string }

func (a wsAddr) Network() string { return "ws" }
func (a wsAddr) String() string  { return a.s }

// adapter implements net.Conn over a *websocket.Conn. It is the only type in
// this package that touches the WebSocket primitives directly; everything else
// (yamux, grpc) sees a plain net.Conn.
type adapter struct {
	conn *websocket.Conn

	// readMu guards rdr; in normal operation only the yamux recvLoop calls
	// Read, but defensive locking is cheap and rules out a class of bugs.
	readMu sync.Mutex
	rdr    io.Reader // the io.Reader for the currently-being-drained message

	// writeMu serialises WriteMessage calls. gorilla does not allow concurrent
	// writes to the same conn; yamux has its own sendLoop that should not race
	// here, but defence in depth makes the contract explicit.
	writeMu sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}
}

// WrapConn adapts a *websocket.Conn into a net.Conn.
//
// Caller transfers ownership of conn to the adapter: closing the returned
// net.Conn closes the underlying WebSocket. Both endpoints (Hub and Worker)
// must use this adapter so the WebSocket data-message convention (binary,
// streamed via NextReader) is symmetric.
func WrapConn(conn *websocket.Conn) net.Conn {
	return &adapter{
		conn:   conn,
		closed: make(chan struct{}),
	}
}

func (a *adapter) Read(p []byte) (int, error) {
	a.readMu.Lock()
	defer a.readMu.Unlock()

	for {
		if a.rdr == nil {
			mt, r, err := a.conn.NextReader()
			if err != nil {
				return 0, translateReadErr(err)
			}
			// We only carry yamux data over BinaryMessage. Drop any TextMessage
			// that somehow slipped through (a sane peer never sends one).
			if mt != websocket.BinaryMessage {
				continue
			}
			a.rdr = r
		}

		n, err := a.rdr.Read(p)
		if errors.Is(err, io.EOF) {
			// Current message drained. Loop to wait for the next.
			a.rdr = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (a *adapter) Write(p []byte) (int, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if err := a.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (a *adapter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		close(a.closed)
		err = a.conn.Close()
	})
	return err
}

func (a *adapter) LocalAddr() net.Addr {
	if addr := a.conn.LocalAddr(); addr != nil {
		return addr
	}
	return wsAddr{s: "ws-local"}
}

func (a *adapter) RemoteAddr() net.Addr {
	if addr := a.conn.RemoteAddr(); addr != nil {
		return addr
	}
	return wsAddr{s: "ws-remote"}
}

func (a *adapter) SetDeadline(t time.Time) error {
	if err := a.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return a.conn.SetWriteDeadline(t)
}

func (a *adapter) SetReadDeadline(t time.Time) error  { return a.conn.SetReadDeadline(t) }
func (a *adapter) SetWriteDeadline(t time.Time) error { return a.conn.SetWriteDeadline(t) }

// translateReadErr normalises gorilla-specific errors into the net.Conn
// contract (io.EOF on close, clean or abrupt). yamux treats every non-nil
// Read error as session-end; surfacing a gorilla "abnormal closure" instead
// of EOF makes the session log spew unnecessary noise.
//
// Specifically, both clean close (1000 normal / 1001 going-away / 1005 no
// status) and abrupt close (1006 abnormal, returned when the underlying TCP
// drops without a Close frame) are mapped to io.EOF: once the peer is gone
// there is nothing more to read, and yamux only needs to know that.
func translateReadErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if errors.Is(err, net.ErrClosed) {
		return io.EOF
	}
	// Any gorilla CloseError — including 1006 "abnormal closure" surfaced for
	// a TCP-level drop — is end-of-stream from yamux's point of view.
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
		return io.EOF
	}
	return err
}
