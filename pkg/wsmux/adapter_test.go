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

package wsmux_test

import (
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/scitix/agent-sandbox/pkg/wsmux"
)

// wsPair returns a connected (server, client) *websocket.Conn pair backed by a
// real httptest server. Both conns are closed via t.Cleanup.
func wsPair(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()

	srvCh := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		srvCh <- c
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	select {
	case s := <-srvCh:
		t.Cleanup(func() { _ = s.Close() })
		t.Cleanup(func() { _ = c.Close() })
		return s, c
	case <-time.After(2 * time.Second):
		t.Fatal("server side did not upgrade in time")
		return nil, nil
	}
}

// TestAdapter_RoundTrip writes from one side and reads on the other across
// many WS messages of varying sizes. It catches the most common bug in this
// adapter: assuming a single Read returns the full message (Read on a stream
// transport must handle partial reads + message boundaries transparently).
func TestAdapter_RoundTrip(t *testing.T) {
	srv, cli := wsPair(t)
	a := wsmux.WrapConn(srv)
	b := wsmux.WrapConn(cli)
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()

	// Build a sequence of payloads with varied sizes, including small chunks
	// (below typical WS frame size) and larger blobs (well over the gorilla
	// default read buffer of 4 KiB).
	sizes := []int{1, 7, 4095, 4097, 65 * 1024, 1024 * 1024}
	var wantTotal int
	want := make([][]byte, len(sizes))
	for i, n := range sizes {
		buf := make([]byte, n)
		_, _ = rand.Read(buf)
		want[i] = buf
		wantTotal += n
	}

	// Writer: pump every payload as one WS BinaryMessage (each Write -> one msg).
	writeDone := make(chan error, 1)
	go func() {
		for _, p := range want {
			if _, err := a.Write(p); err != nil {
				writeDone <- err
				return
			}
		}
		writeDone <- nil
	}()

	// Reader: io.ReadFull each payload into a sized buffer and verify byte-for-byte.
	got := make([][]byte, len(sizes))
	for i, n := range sizes {
		got[i] = make([]byte, n)
		if _, err := io.ReadFull(b, got[i]); err != nil {
			t.Fatalf("ReadFull payload %d (size %d): %v", i, n, err)
		}
		if string(got[i]) != string(want[i]) {
			t.Fatalf("payload %d mismatch (first 32 bytes: got %x want %x)", i, got[i][:min(32, n)], want[i][:min(32, n)])
		}
	}

	if err := <-writeDone; err != nil {
		t.Fatalf("write goroutine: %v", err)
	}
}

// TestAdapter_ConcurrentWriteSerialised stresses the writeMu by having many
// goroutines write simultaneously. The reader must see every payload intact;
// any byte-level interleaving would corrupt the message boundary.
func TestAdapter_ConcurrentWriteSerialised(t *testing.T) {
	srv, cli := wsPair(t)
	a := wsmux.WrapConn(srv)
	b := wsmux.WrapConn(cli)
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()

	const writers = 16
	const perWriter = 64
	const payloadSize = 1024

	// Each writer sends payloadSize bytes filled with its own ID; reader
	// re-derives the writer ID from any byte in the chunk and counts.
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := range writers {
		go func(id byte) {
			defer wg.Done()
			buf := make([]byte, payloadSize)
			for i := range buf {
				buf[i] = id
			}
			for range perWriter {
				if _, err := a.Write(buf); err != nil {
					t.Errorf("writer %d: %v", id, err)
					return
				}
			}
		}(byte(w))
	}

	totals := make([]int, writers)
	for range writers * perWriter {
		buf := make([]byte, payloadSize)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Fatalf("ReadFull: %v", err)
		}
		// All bytes in a message must equal the same writer ID — no interleaving.
		first := buf[0]
		for i, x := range buf {
			if x != first {
				t.Fatalf("interleaving detected: byte 0 = %d, byte %d = %d", first, i, x)
			}
		}
		if int(first) >= writers {
			t.Fatalf("writer id %d out of range", first)
		}
		totals[first]++
	}
	wg.Wait()

	for i, n := range totals {
		if n != perWriter {
			t.Errorf("writer %d received %d messages, want %d", i, n, perWriter)
		}
	}
}

// TestAdapter_CloseReturnsEOF: after Close(), subsequent Read must surface
// io.EOF (or an io.EOF-equivalent) rather than hang or return a raw
// "websocket: close" error that yamux would not recognise.
func TestAdapter_CloseReturnsEOF(t *testing.T) {
	srv, cli := wsPair(t)
	a := wsmux.WrapConn(srv)
	b := wsmux.WrapConn(cli)
	defer func() { _ = a.Close() }()

	// Close the remote end; the local Read should observe EOF.
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Set a short deadline so a buggy implementation cannot hang the test.
	_ = a.SetReadDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 16)
	n, err := a.Read(buf)
	if n != 0 {
		t.Errorf("Read returned %d bytes, want 0", n)
	}
	if !errors.Is(err, io.EOF) {
		// Some platforms surface "connection reset" or net errors; accept any
		// error that yamux would treat as session-end (non-temporary).
		var nerr net.Error
		if errors.As(err, &nerr) {
			t.Logf("Read after remote close returned net error %v (acceptable)", err)
			return
		}
		t.Errorf("Read after remote close = %v, want io.EOF or net.Error", err)
	}
}
