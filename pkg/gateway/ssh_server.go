package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
)

// SSHServer bridges SSH sessions to sidecar InteractiveShell gRPC streams.
type SSHServer struct {
	gw       *Gateway
	config   *ssh.ServerConfig
	listener net.Listener
	port     int
	wg       sync.WaitGroup
	stopCh   chan struct{}
}

// NewSSHServer creates an SSH server that authenticates sessions by username (sessionID).
// If password is empty, any password is accepted as long as the session exists.
func NewSSHServer(gw *Gateway, port int, hostKeyPath string, password string) (*SSHServer, error) {
	hostKey, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}

	s := &SSHServer{
		gw:     gw,
		port:   port,
		stopCh: make(chan struct{}),
	}

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			sessionID := conn.User()
			if _, err := gw.GetSession(sessionID); err != nil {
				return nil, fmt.Errorf("session not found")
			}
			if password != "" && string(pass) != password {
				return nil, fmt.Errorf("invalid password")
			}
			return nil, nil
		},
	}
	sshConfig.AddHostKey(hostKey)

	s.config = sshConfig
	return s, nil
}

// Start begins accepting SSH connections. It blocks until Stop is called.
func (s *SSHServer) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listen on :%d: %w", s.port, err)
	}
	s.listener = ln
	log.Printf("SSH server listening on :%d", s.port)

	go s.acceptLoop()
	return nil
}

// Stop gracefully shuts down the SSH server.
func (s *SSHServer) Stop() {
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

func (s *SSHServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				log.Printf("SSH accept error: %v", err)
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(conn)
		}()
	}
}

func (s *SSHServer) handleConnection(netConn net.Conn) {
	defer netConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		log.Printf("SSH handshake failed: %v", err)
		return
	}
	defer sshConn.Close()

	sessionID := sshConn.User()
	log.Printf("SSH connection from %s for session %s", sshConn.RemoteAddr(), sessionID)

	// Discard global requests (keepalive, etc.)
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("SSH channel accept failed: %v", err)
			return
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleChannel(sessionID, channel, requests)
		}()
	}
}

func (s *SSHServer) handleChannel(sessionID string, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	info, err := s.gw.GetSession(sessionID)
	if err != nil {
		fmt.Fprintf(channel, "session not found: %s\r\n", sessionID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shellStream, err := s.gw.sidecarClient.InteractiveShell(
		ctx,
		info.PodIP,
	)
	if err != nil {
		fmt.Fprintf(channel, "failed to open shell: %v\r\n", err)
		return
	}
	defer shellStream.Close()

	done := make(chan struct{})

	// gRPC -> SSH (stdout)
	go func() {
		defer close(done)
		for {
			out, recvErr := shellStream.Recv()
			if recvErr != nil {
				if recvErr != io.EOF {
					log.Printf("SSH shell stream recv error (session=%s): %v", sessionID, recvErr)
				}
				return
			}
			if out.Closed {
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(out.ExitCode)}))
				return
			}
			if _, writeErr := channel.Write([]byte(out.Data)); writeErr != nil {
				return
			}
		}
	}()

	// Handle SSH requests (pty-req, shell, window-change, signal)
	go func() {
		for req := range requests {
			switch req.Type {
			case "pty-req":
				// Parse PTY request for initial terminal size
				pty := parsePtyRequest(req.Payload)
				if pty.Cols > 0 && pty.Rows > 0 {
					shellStream.Send(interfaces.ShellInput{
						Resize: true,
						Rows:   int32(pty.Rows),
						Cols:   int32(pty.Cols),
					})
				}
				if req.WantReply {
					req.Reply(true, nil)
				}

			case "shell":
				if req.WantReply {
					req.Reply(true, nil)
				}

			case "window-change":
				wc := parseWindowChange(req.Payload)
				shellStream.Send(interfaces.ShellInput{
					Resize: true,
					Rows:   int32(wc.Rows),
					Cols:   int32(wc.Cols),
				})

			case "signal":
				sig := parseSignal(req.Payload)
				if sig != "" {
					shellStream.Send(interfaces.ShellInput{
						Signal: sig,
					})
				}

			case "exec":
				// We only support interactive shell, not exec
				if req.WantReply {
					req.Reply(false, nil)
				}

			default:
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}
	}()

	// SSH -> gRPC (stdin)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := channel.Read(buf)
			if readErr != nil {
				shellStream.Close()
				return
			}
			if sendErr := shellStream.Send(interfaces.ShellInput{
				Data: string(buf[:n]),
			}); sendErr != nil {
				return
			}
		}
	}()

	// Wait for shell to close
	<-done
}

// ptyRequest holds parsed PTY request fields.
type ptyRequest struct {
	Term   string
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

func parsePtyRequest(payload []byte) ptyRequest {
	var req struct {
		Term   string
		Cols   uint32
		Rows   uint32
		Width  uint32
		Height uint32
	}
	if err := ssh.Unmarshal(payload, &req); err != nil {
		return ptyRequest{}
	}
	return ptyRequest{
		Term:   req.Term,
		Cols:   req.Cols,
		Rows:   req.Rows,
		Width:  req.Width,
		Height: req.Height,
	}
}

type windowChange struct {
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

func parseWindowChange(payload []byte) windowChange {
	var wc windowChange
	if err := ssh.Unmarshal(payload, &wc); err != nil {
		return windowChange{}
	}
	return wc
}

func parseSignal(payload []byte) string {
	var sig struct {
		Signal string
	}
	if err := ssh.Unmarshal(payload, &sig); err != nil {
		return ""
	}
	// SSH signal names don't have the "SIG" prefix; add it for consistency
	// with the gRPC protocol which expects e.g., "SIGINT"
	return "SIG" + sig.Signal
}

// loadOrGenerateHostKey loads an SSH host key from path, or generates a new
// Ed25519 key and saves it if the file does not exist.
func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(data)
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read host key: %w", err)
	}

	// Generate new Ed25519 key
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}

	pemData := pem.EncodeToMemory(pemBlock)

	// Ensure parent directory exists
	if dir := filepath.Dir(path); dir != "" {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return nil, fmt.Errorf("create host key directory: %w", mkErr)
		}
	}

	if err := os.WriteFile(path, pemData, 0o600); err != nil {
		return nil, fmt.Errorf("write host key: %w", err)
	}

	log.Printf("Generated SSH host key at %s", path)
	return ssh.ParsePrivateKey(pemData)
}
