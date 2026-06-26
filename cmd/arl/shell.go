package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

func runShell(sessionID string) error {
	wsURL := strings.Replace(flagGatewayURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.TrimRight(wsURL, "/") + "/v1/sessions/" + sessionID + "/shell"

	header := http.Header{}
	if flagAPIKey != "" {
		header.Set("Authorization", "Bearer "+flagAPIKey)
	}

	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("parse WebSocket URL: %w", err)
	}
	if flagAPIKey != "" {
		q := u.Query()
		q.Set("token", flagAPIKey)
		u.RawQuery = q.Encode()
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return fmt.Errorf("connect to shell: %w", err)
	}
	defer conn.Close()

	// Set terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("set raw terminal: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})

	// Read from WebSocket -> stdout
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			os.Stdout.Write(msg)
		}
	}()

	// Read from stdin -> WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					return
				}
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
		}
	}()

	select {
	case <-done:
	case <-sigCh:
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}

	return nil
}
