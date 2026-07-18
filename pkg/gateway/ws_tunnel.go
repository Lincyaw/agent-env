package gateway

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

const (
	tunnelDialTimeout = 10 * time.Second
	tunnelMaxPort     = 65535
)

func handleTunnel(gw *Gateway, authCfg *AuthConfig) http.HandlerFunc {
	upgrader := newUpgrader(authCfg)

	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		portStr := chi.URLParam(r, "port")

		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > tunnelMaxPort {
			http.Error(w, "invalid port", http.StatusBadRequest)
			return
		}

		_, podIP, releaseSession, err := gw.acquireSessionPodIP(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer releaseSession()

		// Dial the target TCP port on the pod
		target := net.JoinHostPort(podIP, strconv.Itoa(port))
		tcpConn, err := net.DialTimeout("tcp", target, tunnelDialTimeout)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot connect to %s: %v", target, err), http.StatusBadGateway)
			return
		}
		defer tcpConn.Close()

		// Upgrade HTTP to WebSocket
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket tunnel upgrade failed for session %s port %d: %v", id, port, err)
			return
		}
		defer ws.Close()

		// Bidirectional relay: WebSocket <-> TCP
		var wg sync.WaitGroup
		wg.Add(2)

		// TCP -> WebSocket
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for {
				n, readErr := tcpConn.Read(buf)
				if n > 0 {
					if writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
						return
					}
				}
				if readErr != nil {
					ws.WriteMessage(websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
					return
				}
			}
		}()

		// WebSocket -> TCP
		go func() {
			defer wg.Done()
			for {
				_, msg, readErr := ws.ReadMessage()
				if readErr != nil {
					tcpConn.Close()
					return
				}
				if _, writeErr := tcpConn.Write(msg); writeErr != nil {
					return
				}
			}
		}()

		wg.Wait()
	}
}

// wsNetConn wraps a gorilla/websocket.Conn as a net.Conn-like writer for io.Copy.
// Used internally; exported only for testing if needed.
type wsNetConn struct {
	ws *websocket.Conn
}

func (w *wsNetConn) Write(p []byte) (int, error) {
	err := w.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *wsNetConn) Read(p []byte) (int, error) {
	_, reader, err := w.ws.NextReader()
	if err != nil {
		return 0, err
	}
	return reader.Read(p)
}

var _ io.ReadWriter = (*wsNetConn)(nil)
