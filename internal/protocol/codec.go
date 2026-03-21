package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxMessageSize = 10 * 1024 * 1024 // 10MB

// WriteMessage sends a length-prefixed JSON message to w.
func WriteMessage(w io.Writer, msg *Envelope) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write length prefix: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// ReadMessage reads a length-prefixed JSON message from r.
func ReadMessage(r io.Reader) (*Envelope, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("read length prefix: %w", err)
	}
	if length > maxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", length, maxMessageSize)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &env, nil
}

// DecodePayload extracts a typed payload from a raw Envelope.
// The Envelope.Payload is initially decoded as a json.RawMessage-like map,
// so we re-marshal and unmarshal into the target type.
func DecodePayload[T any](env *Envelope) (*T, error) {
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("re-marshal payload: %w", err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode payload as %T: %w", result, err)
	}
	return &result, nil
}
