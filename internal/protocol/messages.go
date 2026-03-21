// Package protocol defines the vsock wire protocol between host API server
// and in-VM agent. Messages are JSON-encoded with a 4-byte big-endian
// length prefix.
//
// Message flow:
//
//	Host                          Guest Agent
//	  │                               │
//	  │──── PingRequest ─────────────▶│
//	  │◀─── PingResponse ────────────│
//	  │                               │
//	  │──── ExecRequest ─────────────▶│
//	  │◀─── ExecResponse ────────────│
//	  │                               │
//	  │──── FileWriteRequest ────────▶│
//	  │◀─── FileWriteResponse ───────│
//	  │                               │
//	  │──── FileReadRequest ─────────▶│
//	  │◀─── FileReadResponse ────────│
package protocol

// MessageType identifies the kind of vsock message.
type MessageType string

const (
	TypePingRequest  MessageType = "ping_request"
	TypePingResponse MessageType = "ping_response"
	TypeExecRequest  MessageType = "exec_request"
	TypeExecResponse      MessageType = "exec_response"
	TypeFileWriteRequest  MessageType = "file_write_request"
	TypeFileWriteResponse MessageType = "file_write_response"
	TypeFileReadRequest   MessageType = "file_read_request"
	TypeFileReadResponse  MessageType = "file_read_response"
	TypeError             MessageType = "error"
)

// Envelope wraps every vsock message with a type discriminator.
type Envelope struct {
	Type    MessageType `json:"type"`
	Payload any         `json:"payload"`
}

// PingRequest is a health check from host to agent.
type PingRequest struct{}

// PingResponse confirms the agent is alive.
type PingResponse struct {
	Version string `json:"version"`
}

// ExecRequest asks the agent to run a command.
type ExecRequest struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	WorkDir string            `json:"workdir,omitempty"`
	Timeout int               `json:"timeout,omitempty"` // seconds, 0 = no timeout
}

// ExecResponse returns the result of a command execution.
type ExecResponse struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// FileWriteRequest asks the agent to write a file inside the VM.
type FileWriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"` // base64-encoded for binary, raw for text
	Mode    int    `json:"mode"`    // file permissions (e.g., 0644)
	Binary  bool   `json:"binary"`  // if true, Content is base64-encoded
}

// FileWriteResponse confirms the file was written.
type FileWriteResponse struct {
	BytesWritten int `json:"bytes_written"`
}

// FileReadRequest asks the agent to read a file from the VM.
type FileReadRequest struct {
	Path string `json:"path"`
}

// FileReadResponse returns the file contents.
type FileReadResponse struct {
	Content string `json:"content"` // base64-encoded
	Size    int64  `json:"size"`
	Mode    int    `json:"mode"`
}

// ErrorResponse is returned when the agent encounters an error.
type ErrorResponse struct {
	Message string `json:"message"`
}
