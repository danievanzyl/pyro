package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteReadMessage(t *testing.T) {
	msg := &Envelope{
		Type: TypeExecRequest,
		Payload: &ExecRequest{
			Command: []string{"echo", "hello"},
			Env:     map[string]string{"FOO": "bar"},
			WorkDir: "/tmp",
			Timeout: 30,
		},
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if got.Type != TypeExecRequest {
		t.Errorf("type = %q, want %q", got.Type, TypeExecRequest)
	}

	req, err := DecodePayload[ExecRequest](got)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	if len(req.Command) != 2 || req.Command[0] != "echo" || req.Command[1] != "hello" {
		t.Errorf("command = %v, want [echo hello]", req.Command)
	}
	if req.Env["FOO"] != "bar" {
		t.Errorf("env FOO = %q, want %q", req.Env["FOO"], "bar")
	}
	if req.WorkDir != "/tmp" {
		t.Errorf("workdir = %q, want %q", req.WorkDir, "/tmp")
	}
	if req.Timeout != 30 {
		t.Errorf("timeout = %d, want 30", req.Timeout)
	}
}

func TestWriteReadPingRoundtrip(t *testing.T) {
	msg := &Envelope{
		Type:    TypePingResponse,
		Payload: &PingResponse{Version: "0.1.0"},
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypePingResponse {
		t.Errorf("type = %q, want %q", got.Type, TypePingResponse)
	}
	resp, err := DecodePayload[PingResponse](got)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", resp.Version, "0.1.0")
	}
}

func TestReadMessageTooLarge(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(maxMessageSize+1))
	buf.Write(make([]byte, 100)) // doesn't matter, should fail before reading

	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestReadMessageMalformedJSON(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("{bad json")
	binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	buf.Write(payload)

	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestReadMessageEmptyReader(t *testing.T) {
	var buf bytes.Buffer
	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for empty reader")
	}
}
