package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// newUpgrader returns a WebSocket Upgrader with origin validation.
// When allowedOrigins is non-empty, only those origins are accepted.
// When it is empty and auth is enabled, all origins are rejected (no
// unauthenticated browser access). When auth is disabled, all origins
// are allowed for backward compatibility.
func newUpgrader(authCfg *AuthConfig) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if authCfg == nil || !authCfg.Enabled {
				return true
			}
			if len(authCfg.AllowedOrigins) == 0 {
				return false
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Non-browser clients (curl, SDKs) typically don't send Origin.
				return true
			}
			parsed, err := url.Parse(origin)
			if err != nil {
				return false
			}
			host := strings.ToLower(parsed.Host)
			for _, allowed := range authCfg.AllowedOrigins {
				if strings.ToLower(allowed) == host {
					return true
				}
			}
			return false
		},
	}
}

// wsMessage is the JSON envelope for WebSocket messages.
type wsMessage struct {
	Type     string `json:"type"`                // "input", "output", "signal", "resize", "exit"
	Data     string `json:"data,omitempty"`      // stdin/stdout data
	Signal   string `json:"signal,omitempty"`    // signal name (e.g., "SIGINT")
	Rows     int32  `json:"rows,omitempty"`      // terminal rows
	Cols     int32  `json:"cols,omitempty"`      // terminal columns
	ExitCode int32  `json:"exit_code,omitempty"` // exit code
}

// handleShell upgrades to WebSocket and proxies to sidecar InteractiveShell gRPC stream.
func handleShell(gw *Gateway, authCfg *AuthConfig) http.HandlerFunc {
	upgrader := newUpgrader(authCfg)

	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		s, ok := gw.store.Get(id)
		if !ok {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		s.mu.RLock()
		podIP := s.Info.PodIP
		ownerHash := s.ownerKeyHash
		s.mu.RUnlock()

		if err := CheckSessionOwnership(r.Context(), ownerHash); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		// Upgrade HTTP to WebSocket
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer ws.Close()

		// Open gRPC bidi stream to sidecar with cancellable context
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		shellStream, err := gw.sidecarClient.InteractiveShell(ctx, podIP)
		if err != nil {
			writeWSError(ws, "failed to open shell: "+err.Error())
			return
		}
		defer shellStream.Close()

		done := make(chan struct{})

		// gRPC -> WebSocket: read from sidecar, send to client
		go func() {
			defer close(done)
			for {
				out, recvErr := shellStream.Recv()
				if recvErr != nil {
					if recvErr != io.EOF {
						writeWSError(ws, "shell stream error: "+recvErr.Error())
					}
					return
				}

				var msg wsMessage
				if out.Closed {
					msg = wsMessage{Type: "exit", ExitCode: out.ExitCode}
				} else {
					msg = wsMessage{Type: "output", Data: out.Data}
				}

				data, _ := json.Marshal(msg)
				if writeErr := ws.WriteMessage(websocket.TextMessage, data); writeErr != nil {
					return
				}

				if out.Closed {
					return
				}
			}
		}()

		// WebSocket -> gRPC: read from client, send to sidecar
		go func() {
			for {
				_, rawMsg, readErr := ws.ReadMessage()
				if readErr != nil {
					shellStream.Close()
					return
				}

				var msg wsMessage
				if err := json.Unmarshal(rawMsg, &msg); err != nil {
					continue
				}

				var input interfaces.ShellInput
				switch msg.Type {
				case "input":
					input.Data = msg.Data
				case "signal":
					input.Signal = msg.Signal
				case "resize":
					input.Resize = true
					input.Rows = msg.Rows
					input.Cols = msg.Cols
				default:
					continue
				}

				if sendErr := shellStream.Send(input); sendErr != nil {
					return
				}
			}
		}()

		// Wait for shell to close
		<-done
	}
}

func writeWSError(ws *websocket.Conn, errMsg string) {
	msg, _ := json.Marshal(wsMessage{Type: "error", Data: errMsg})
	ws.WriteMessage(websocket.TextMessage, msg)
}
