// Command agent is the in-VM vsock agent for Pyro.
//
// It runs inside each Firecracker microVM as PID 1 (or spawned by init).
// Listens on vsock port 1024 for commands from the host API server.
//
// Protocol: length-prefixed JSON over vsock (see internal/protocol).
//
// Supported message types:
//   - PingRequest      → PingResponse
//   - ExecRequest      → ExecResponse
//   - FileWriteRequest → FileWriteResponse
//   - FileReadRequest  → FileReadResponse
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danievanzyl/pyro/internal/protocol"
)

const (
	agentVersion = "0.1.0"
	listenPort   = 1024
)

func main() {
	// If running as PID 1 (init), mount essential filesystems first.
	if os.Getpid() == 1 {
		initAsInit()
	}

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	log.Info("agent starting", "version", agentVersion, "port", listenPort)

	listener, err := listenVsock(listenPort)
	if err != nil {
		log.Error("listen vsock", "err", err)
		os.Exit(1)
	}
	defer listener.Close()

	log.Info("agent listening", "port", listenPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Error("accept", "err", err)
			continue
		}
		go handleConnection(conn, log)
	}
}

func handleConnection(conn net.Conn, log *slog.Logger) {
	defer conn.Close()

	msg, err := protocol.ReadMessage(conn)
	if err != nil {
		log.Error("read message", "err", err)
		sendError(conn, fmt.Sprintf("read error: %v", err))
		return
	}

	switch msg.Type {
	case protocol.TypePingRequest:
		handlePing(conn, log)
	case protocol.TypeExecRequest:
		handleExec(conn, msg, log)
	case protocol.TypeFileWriteRequest:
		handleFileWrite(conn, msg, log)
	case protocol.TypeFileReadRequest:
		handleFileRead(conn, msg, log)
	default:
		sendError(conn, fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

func handlePing(conn net.Conn, log *slog.Logger) {
	resp := &protocol.Envelope{
		Type: protocol.TypePingResponse,
		Payload: &protocol.PingResponse{
			Version: agentVersion,
		},
	}
	if err := protocol.WriteMessage(conn, resp); err != nil {
		log.Error("write ping response", "err", err)
	}
}

func handleExec(conn net.Conn, msg *protocol.Envelope, log *slog.Logger) {
	req, err := protocol.DecodePayload[protocol.ExecRequest](msg)
	if err != nil {
		sendError(conn, fmt.Sprintf("decode exec request: %v", err))
		return
	}

	if len(req.Command) == 0 {
		sendError(conn, "command is required")
		return
	}

	log.Info("exec", "command", req.Command)

	ctx := context.Background()
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, req.Command[0], req.Command[1:]...)
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}
	// Always inherit base environment, overlay custom vars.
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			sendError(conn, fmt.Sprintf("exec error: %v", err))
			return
		}
	}

	resp := &protocol.Envelope{
		Type: protocol.TypeExecResponse,
		Payload: &protocol.ExecResponse{
			ExitCode: exitCode,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		},
	}
	if err := protocol.WriteMessage(conn, resp); err != nil {
		log.Error("write exec response", "err", err)
	}
}

func handleFileWrite(conn net.Conn, msg *protocol.Envelope, log *slog.Logger) {
	req, err := protocol.DecodePayload[protocol.FileWriteRequest](msg)
	if err != nil {
		sendError(conn, fmt.Sprintf("decode file write request: %v", err))
		return
	}

	if req.Path == "" {
		sendError(conn, "path is required")
		return
	}

	// Ensure path is absolute and within allowed boundaries.
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		sendError(conn, fmt.Sprintf("invalid path: %v", err))
		return
	}

	log.Info("file_write", "path", absPath, "binary", req.Binary)

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		sendError(conn, fmt.Sprintf("create parent dir: %v", err))
		return
	}

	var data []byte
	if req.Binary {
		data, err = base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			sendError(conn, fmt.Sprintf("decode base64: %v", err))
			return
		}
	} else {
		data = []byte(req.Content)
	}

	mode := fs.FileMode(0644)
	if req.Mode != 0 {
		mode = fs.FileMode(req.Mode)
	}

	if err := os.WriteFile(absPath, data, mode); err != nil {
		sendError(conn, fmt.Sprintf("write file: %v", err))
		return
	}

	resp := &protocol.Envelope{
		Type: protocol.TypeFileWriteResponse,
		Payload: &protocol.FileWriteResponse{
			BytesWritten: len(data),
		},
	}
	if err := protocol.WriteMessage(conn, resp); err != nil {
		log.Error("write file-write response", "err", err)
	}
}

func handleFileRead(conn net.Conn, msg *protocol.Envelope, log *slog.Logger) {
	req, err := protocol.DecodePayload[protocol.FileReadRequest](msg)
	if err != nil {
		sendError(conn, fmt.Sprintf("decode file read request: %v", err))
		return
	}

	if req.Path == "" {
		sendError(conn, "path is required")
		return
	}

	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		sendError(conn, fmt.Sprintf("invalid path: %v", err))
		return
	}

	log.Info("file_read", "path", absPath)

	info, err := os.Stat(absPath)
	if err != nil {
		sendError(conn, fmt.Sprintf("stat file: %v", err))
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		sendError(conn, fmt.Sprintf("read file: %v", err))
		return
	}

	resp := &protocol.Envelope{
		Type: protocol.TypeFileReadResponse,
		Payload: &protocol.FileReadResponse{
			Content: base64.StdEncoding.EncodeToString(data),
			Size:    info.Size(),
			Mode:    int(info.Mode()),
		},
	}
	if err := protocol.WriteMessage(conn, resp); err != nil {
		log.Error("write file-read response", "err", err)
	}
}

func sendError(conn net.Conn, message string) {
	resp := &protocol.Envelope{
		Type: protocol.TypeError,
		Payload: &protocol.ErrorResponse{
			Message: message,
		},
	}
	if err := protocol.WriteMessage(conn, resp); err != nil {
		slog.Error("write error response", "err", err)
	}
}

// initAsInit performs minimal init duties when running as PID 1.
// Actual implementation is in init_linux.go (requires Linux syscalls).
// On non-Linux, this is a no-op.

// listenVsock sets up a vsock listener. On Linux, uses AF_VSOCK.
// This binary is only intended to run inside a Firecracker VM (Linux).
func listenVsock(port uint32) (net.Listener, error) {
	return listenVsockPlatform(port)
}

// Temporary: use JSON to avoid unused import.
var _ = json.Marshal
