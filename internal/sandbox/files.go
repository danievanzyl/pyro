package sandbox

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/danievanzyl/pyro/internal/protocol"
)

// WriteFileInSandbox sends a file to the in-VM agent for writing.
func (m *Manager) WriteFileInSandbox(ctx context.Context, id string, req *protocol.FileWriteRequest) (*protocol.FileWriteResponse, error) {
	conn, err := m.connectSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	env := &protocol.Envelope{
		Type:    protocol.TypeFileWriteRequest,
		Payload: req,
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return nil, fmt.Errorf("send file write request: %w", err)
	}

	resp, err := protocol.ReadMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("read file write response: %w", err)
	}

	if resp.Type == protocol.TypeError {
		errResp, _ := protocol.DecodePayload[protocol.ErrorResponse](resp)
		if errResp != nil {
			return nil, fmt.Errorf("agent error: %s", errResp.Message)
		}
		return nil, fmt.Errorf("agent returned error")
	}

	result, err := protocol.DecodePayload[protocol.FileWriteResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// ReadFileFromSandbox reads a file from the in-VM agent.
func (m *Manager) ReadFileFromSandbox(ctx context.Context, id string, req *protocol.FileReadRequest) (*protocol.FileReadResponse, error) {
	conn, err := m.connectSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	env := &protocol.Envelope{
		Type:    protocol.TypeFileReadRequest,
		Payload: req,
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return nil, fmt.Errorf("send file read request: %w", err)
	}

	resp, err := protocol.ReadMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("read file read response: %w", err)
	}

	if resp.Type == protocol.TypeError {
		errResp, _ := protocol.DecodePayload[protocol.ErrorResponse](resp)
		if errResp != nil {
			return nil, fmt.Errorf("agent error: %s", errResp.Message)
		}
		return nil, fmt.Errorf("agent returned error")
	}

	result, err := protocol.DecodePayload[protocol.FileReadResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

// connectSandbox establishes a vsock connection to a sandbox's agent.
func (m *Manager) connectSandbox(ctx context.Context, id string) (net.Conn, error) {
	m.mu.RLock()
	handle, ok := m.active[id]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("sandbox %s not in active set", id)
	}
	if handle.sandbox.IsExpired() {
		return nil, fmt.Errorf("sandbox %s has expired", id)
	}

	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	conn, err := m.dialVsock(handle.sandbox.VsockCID, m.cfg.VsockAgentPort)
	if err != nil {
		return nil, fmt.Errorf("connect vsock: %w", err)
	}
	conn.SetDeadline(time.Now().Add(timeout))
	return conn, nil
}
