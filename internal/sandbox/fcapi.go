package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

// firecrackerAPICall makes an HTTP request to a Firecracker instance's
// Unix socket API. Used for operations like pause, snapshot, etc.
func firecrackerAPICall(socketPath, method, path, body string) error {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, "http://localhost"+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s returned %d: %s", method, path, resp.StatusCode, respBody)
	}

	return nil
}

// newFirecrackerCmd creates an exec.Cmd for starting Firecracker.
func newFirecrackerCmd(ctx context.Context, bin, socketPath, configPath, workDir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin,
		"--api-sock", socketPath,
		"--config-file", configPath,
	)
	cmd.Dir = workDir
	return cmd
}
