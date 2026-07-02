package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

type shellWSMessage struct {
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	Signal   string `json:"signal,omitempty"`
	Rows     int32  `json:"rows,omitempty"`
	Cols     int32  `json:"cols,omitempty"`
	ExitCode int32  `json:"exit_code,omitempty"`
}

type shellExitError struct {
	code int32
}

func (e *shellExitError) Error() string {
	return fmt.Sprintf("shell exited with status %d", e.code)
}

func (e *shellExitError) ExitCode() int {
	if e.code >= 1 && e.code <= 125 {
		return int(e.code)
	}
	return exitGeneric
}

func encodeShellWSMessage(msg shellWSMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func decodeShellWSMessage(raw []byte) (shellWSMessage, error) {
	var msg shellWSMessage
	err := json.Unmarshal(raw, &msg)
	return msg, err
}

func runShell(sessionID string) error {
	wsURL := strings.Replace(flagGatewayURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.TrimRight(wsURL, "/") + "/v1/sessions/" + sessionID + "/shell"

	apiKey := effectiveAPIKey()
	header := http.Header{}
	if apiKey != "" {
		header.Set("Authorization", "Bearer "+apiKey)
	}

	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("parse WebSocket URL: %w", err)
	}
	if apiKey != "" {
		q := u.Query()
		q.Set("token", apiKey)
		u.RawQuery = q.Encode()
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return fmt.Errorf("connect to shell: %w", err)
	}
	defer conn.Close()

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return &cliError{code: exitEnvironment, err: fmt.Errorf("stdin is not a terminal; use session exec for non-interactive commands")}
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("set raw terminal: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	var writeMu sync.Mutex
	writeMessage := func(msg shellWSMessage) error {
		data, err := encodeShellWSMessage(msg)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, data)
	}
	writeClose := func() {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}

	sendResize := func() error {
		width, height, err := term.GetSize(int(os.Stdin.Fd()))
		if err != nil {
			return nil
		}
		if width <= 0 || height <= 0 {
			return nil
		}
		return writeMessage(shellWSMessage{
			Type: "resize",
			Rows: int32(height),
			Cols: int32(width),
		})
	}

	if err := sendResize(); err != nil {
		return fmt.Errorf("send terminal size: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	defer signal.Stop(resizeCh)

	done := make(chan error, 1)

	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				done <- nil
				return
			}
			msg, err := decodeShellWSMessage(raw)
			if err != nil {
				// Older gateways returned raw terminal bytes. Keep that path usable.
				_, _ = os.Stdout.Write(raw)
				continue
			}
			switch msg.Type {
			case "output":
				_, _ = os.Stdout.Write([]byte(msg.Data))
			case "error":
				done <- errors.New(msg.Data)
				return
			case "exit":
				if msg.ExitCode != 0 {
					done <- &shellExitError{code: msg.ExitCode}
					return
				}
				done <- nil
				return
			default:
				// Unknown message types are ignored for forward compatibility.
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if writeErr := writeMessage(shellWSMessage{Type: "input", Data: string(buf[:n])}); writeErr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					return
				}
				writeClose()
				return
			}
		}
	}()

	for {
		select {
		case err := <-done:
			return err
		case sig := <-sigCh:
			if sig == syscall.SIGINT {
				if err := writeMessage(shellWSMessage{Type: "signal", Signal: "SIGINT"}); err != nil {
					return err
				}
				continue
			}
			writeClose()
			return &cliError{code: exitCancelled, err: fmt.Errorf("shell interrupted")}
		case <-resizeCh:
			if err := sendResize(); err != nil {
				return fmt.Errorf("send terminal size: %w", err)
			}
		}
	}
}
