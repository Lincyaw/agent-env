package client

import (
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/Lincyaw/agent-env/pkg/pb/executorv2"
	"google.golang.org/protobuf/proto"
)

// serveOneResponse answers a single framed request with resp.
func serveOneResponse(t *testing.T, resp *pb.Response) (string, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := readFrame(conn); err != nil {
			return
		}
		data, err := proto.Marshal(resp)
		if err != nil {
			return
		}
		writeFrame(conn, msgTypeResponse, data)
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestAuthErrorDistinguishesAnOutdatedExecutor(t *testing.T) {
	// An executor that predates the Auth request cannot decode the oneof and
	// reports a missing kind. Reporting that as a token problem sends people
	// looking through secrets for a version-skew bug.
	resp := &pb.Response{
		Tag:  0,
		Kind: &pb.Response_Error{Error: &pb.ErrorResponse{Code: 1, Message: "missing request kind"}},
	}
	host, port := serveOneResponse(t, resp)
	c := &TCPExecutorClient{port: port, timeout: 5 * time.Second, authSecret: "master", conns: map[string]net.Conn{}}

	_, err := c.dial(host)
	if err == nil {
		t.Fatal("expected authentication to fail")
	}
	if !strings.Contains(err.Error(), "too old") {
		t.Errorf("error should name the version skew, got: %v", err)
	}
	if strings.Contains(err.Error(), "grpcToken") {
		t.Errorf("error should not blame the token, got: %v", err)
	}
}

func TestAuthErrorReportsATokenMismatch(t *testing.T) {
	resp := &pb.Response{
		Tag:  0,
		Kind: &pb.Response_Error{Error: &pb.ErrorResponse{Code: 7, Message: "authentication failed"}},
	}
	host, port := serveOneResponse(t, resp)
	c := &TCPExecutorClient{port: port, timeout: 5 * time.Second, authSecret: "master", conns: map[string]net.Conn{}}

	_, err := c.dial(host)
	if err == nil {
		t.Fatal("expected authentication to fail")
	}
	if !strings.Contains(err.Error(), "auth.grpcToken") {
		t.Errorf("error should point at the token, got: %v", err)
	}
}
