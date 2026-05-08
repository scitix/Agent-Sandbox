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

package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"

	"github.com/scitix/agent-sandbox/pkg/apiserver/service"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// EndOfTransmission represents the signal for ending the transmission (Ctrl+D).
// Sending this before closing stdin ensures the Kubernetes exec session terminates
// properly and prevents connection leaks.
// References:
// - https://github.com/kubernetes/client-go/issues/554
// - https://github.com/juicedata/juicefs-csi-driver/pull/1053
const EndOfTransmission = "\u0004"

// TerminalMessage is the JSON envelope sent from the browser to the server.
type TerminalMessage struct {
	Op   string `json:"op"`             // "stdin" | "resize"
	Data string `json:"data,omitempty"` // only for "stdin"
	Cols uint16 `json:"cols,omitempty"` // only for "resize"
	Rows uint16 `json:"rows,omitempty"` // only for "resize"
}

// terminalStreamHandler bridges a WebSocket connection to a k8s exec stream.
type terminalStreamHandler struct {
	conn    *websocket.Conn
	sizeCh  chan remotecommand.TerminalSize
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	closeFn context.CancelFunc
}

func newTerminalStreamHandler(conn *websocket.Conn, closeFn context.CancelFunc) *terminalStreamHandler {
	r, w := io.Pipe()
	return &terminalStreamHandler{
		conn:    conn,
		sizeCh:  make(chan remotecommand.TerminalSize, 8),
		stdinR:  r,
		stdinW:  w,
		closeFn: closeFn,
	}
}

// Read implements io.Reader — reads stdin from the WebSocket.
func (t *terminalStreamHandler) Read(p []byte) (int, error) {
	return t.stdinR.Read(p)
}

// Next implements remotecommand.TerminalSizeQueue.
func (t *terminalStreamHandler) Next() *remotecommand.TerminalSize {
	size, ok := <-t.sizeCh
	if !ok {
		return nil
	}
	return &size
}

// startWSReader pumps WebSocket messages into the appropriate pipes/channels.
// It must be run in its own goroutine; it closes stdinW when done.
func (t *terminalStreamHandler) startWSReader() {
	defer func() {
		// Write EOT (Ctrl+D) before closing to signal the shell to exit,
		// preventing leaked exec sessions on the pod side.
		_, _ = t.stdinW.Write([]byte(EndOfTransmission))
		_ = t.stdinW.Close()
		close(t.sizeCh)
		t.closeFn()
	}()
	for {
		msgType, raw, err := t.conn.ReadMessage()
		if err != nil {
			return
		}
		// Binary frames → raw stdin (pass-through for xterm binaryType)
		if msgType == websocket.BinaryMessage {
			_, _ = t.stdinW.Write(raw)
			continue
		}
		// Text frames → JSON envelope
		var msg TerminalMessage
		if jsonErr := json.Unmarshal(raw, &msg); jsonErr != nil {
			continue
		}
		switch msg.Op {
		case "stdin":
			_, _ = t.stdinW.Write([]byte(msg.Data))
		case "resize":
			select {
			case t.sizeCh <- remotecommand.TerminalSize{Width: msg.Cols, Height: msg.Rows}:
			default:
			}
		}
	}
}

// wsWriter implements io.Writer and forwards terminal output to the WebSocket.
type wsWriter struct {
	conn *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// WSTerminalHandler returns a gin.HandlerFunc that handles WebSocket terminal sessions.
func WSTerminalHandler(sandboxSvc service.SandboxService, clientset kubernetes.Interface, restCfg *rest.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		sandboxID := c.Param("sandboxId")
		token := c.Query("token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token query parameter is required"})
			return
		}

		info, appErr := sandboxSvc.ValidateExecToken(token)
		if appErr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": appErr.Message})
			return
		}
		if info.SandboxID != sandboxID {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token does not match sandbox"})
			return
		}

		// Pick first container (or fallback to "")
		container := ""
		if len(info.Containers) > 0 {
			container = info.Containers[0]
		}

		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			klog.ErrorS(err, "WebSocket upgrade failed", "sandboxId", sandboxID)
			return
		}
		defer conn.Close() //nolint:errcheck

		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()

		handler := newTerminalStreamHandler(conn, cancel)
		go handler.startWSReader()

		execReq := clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Name(info.PodName).
			Namespace(info.Namespace).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: container,
				Command:   []string{"sh", "-c", "bash || sh"},
				Stdin:     true,
				Stdout:    true,
				Stderr:    true,
				TTY:       true,
			}, scheme.ParameterCodec)

		executor, execErr := remotecommand.NewSPDYExecutor(restCfg, http.MethodPost, execReq.URL())
		if execErr != nil {
			klog.ErrorS(execErr, "Failed to create SPDY executor", "sandboxId", sandboxID)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"failed to create exec session"}`))
			return
		}

		writer := &wsWriter{conn: conn}
		streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             handler,
			Stdout:            writer,
			Stderr:            writer,
			Tty:               true,
			TerminalSizeQueue: handler,
		})
		if streamErr != nil && ctx.Err() == nil {
			klog.V(1).InfoS("Exec stream ended", "sandboxId", sandboxID, "error", streamErr)
		}
	}
}
