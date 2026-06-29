package main

import (
	"encoding/json"
	"testing"
)

func TestShellWSMessageEncoding(t *testing.T) {
	input, err := encodeShellWSMessage(shellWSMessage{Type: "input", Data: "echo ok\n"})
	if err != nil {
		t.Fatalf("encodeShellWSMessage returned error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatalf("unmarshal encoded input: %v", err)
	}
	if got["type"] != "input" || got["data"] != "echo ok\n" {
		t.Fatalf("encoded input = %#v, want input data envelope", got)
	}

	resize, err := encodeShellWSMessage(shellWSMessage{Type: "resize", Rows: 40, Cols: 120})
	if err != nil {
		t.Fatalf("encode resize returned error: %v", err)
	}
	msg, err := decodeShellWSMessage(resize)
	if err != nil {
		t.Fatalf("decodeShellWSMessage returned error: %v", err)
	}
	if msg.Type != "resize" || msg.Rows != 40 || msg.Cols != 120 {
		t.Fatalf("decoded resize = %#v, want rows=40 cols=120", msg)
	}
}

func TestShellExitErrorUsesRemoteExitCode(t *testing.T) {
	err := &shellExitError{code: 42}
	if got := err.ExitCode(); got != 42 {
		t.Fatalf("ExitCode = %d, want 42", got)
	}
}
