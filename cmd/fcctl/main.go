// Command fcctl is the CLI for managing firecrackerlacker.
//
// Commands:
//
//	fcctl setup                          — one-command host setup
//	fcctl build-kernel                   — build Linux 6.1 kernel for Firecracker
//	fcctl build-image <name|all>         — build rootfs image (minimal/ubuntu/python/node)
//	fcctl create-key [name]              — create API key (direct DB)
//	fcctl sandbox create [--ttl N]       — create sandbox
//	fcctl sandbox list                   — list active sandboxes
//	fcctl sandbox exec <id> <cmd...>     — exec command in sandbox
//	fcctl sandbox kill <id>              — destroy sandbox
//	fcctl images                         — list available images
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
	"github.com/google/uuid"
)

var (
	apiURL = envOr("FCCTL_API", "http://localhost:8080")
	apiKey = os.Getenv("FCCTL_KEY")
	dbPath = envOr("FCCTL_DB", "/var/lib/firecrackerlacker/firecrackerlacker.db")
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "setup":
		setup()
	case "build-kernel":
		buildKernel()
	case "build-image":
		buildImage()
	case "create-key":
		createKey()
	case "sandbox", "sb":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: fcctl sandbox <create|list|exec|kill> [args]")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "create":
			sandboxCreate()
		case "list", "ls":
			sandboxList()
		case "exec":
			sandboxExec()
		case "kill", "rm", "delete":
			sandboxKill()
		default:
			fmt.Fprintf(os.Stderr, "unknown sandbox command: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "images":
		imagesList()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

// --- API-based commands ---

func requireKey() {
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: FCCTL_KEY not set. Export your API key:")
		fmt.Fprintln(os.Stderr, "  export FCCTL_KEY=fclk_...")
		os.Exit(1)
	}
}

func apiRequest(method, path string, body any) ([]byte, int) {
	requireKey()
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, apiURL+"/api"+path, bodyReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode
}

func sandboxCreate() {
	ttl := 3600
	image := "default"
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--ttl":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &ttl)
				i++
			}
		case "--image":
			if i+1 < len(os.Args) {
				image = os.Args[i+1]
				i++
			}
		}
	}

	data, status := apiRequest("POST", "/sandboxes", map[string]any{
		"ttl": ttl, "image": image,
	})
	if status != 201 {
		fmt.Fprintf(os.Stderr, "error (%d): %s\n", status, data)
		os.Exit(1)
	}
	var sb map[string]any
	json.Unmarshal(data, &sb)
	fmt.Printf("%s\n", sb["id"])
}

func sandboxList() {
	data, status := apiRequest("GET", "/sandboxes", nil)
	if status != 200 {
		fmt.Fprintf(os.Stderr, "error (%d): %s\n", status, data)
		os.Exit(1)
	}
	var sandboxes []map[string]any
	json.Unmarshal(data, &sandboxes)

	if len(sandboxes) == 0 {
		fmt.Println("no active sandboxes")
		return
	}
	fmt.Printf("%-14s %-10s %-8s %-6s %s\n", "ID", "IMAGE", "STATE", "PID", "EXPIRES")
	for _, sb := range sandboxes {
		id := sb["id"].(string)
		fmt.Printf("%-14s %-10s %-8s %-6.0f %s\n",
			id[:12],
			sb["image"],
			sb["state"],
			sb["pid"],
			sb["expires_at"],
		)
	}
}

func sandboxExec() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: fcctl sandbox exec <id> <command> [args...]")
		os.Exit(1)
	}
	id := os.Args[3]
	cmd := os.Args[4:]

	data, status := apiRequest("POST", "/sandboxes/"+id+"/exec", map[string]any{
		"command": cmd,
	})
	if status != 200 {
		fmt.Fprintf(os.Stderr, "error (%d): %s\n", status, data)
		os.Exit(1)
	}

	var resp struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	json.Unmarshal(data, &resp)

	if resp.Stdout != "" {
		fmt.Print(resp.Stdout)
	}
	if resp.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Stderr)
	}
	os.Exit(resp.ExitCode)
}

func sandboxKill() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: fcctl sandbox kill <id>")
		os.Exit(1)
	}
	id := os.Args[3]
	_, status := apiRequest("DELETE", "/sandboxes/"+id, nil)
	if status != 204 {
		fmt.Fprintf(os.Stderr, "error: %d\n", status)
		os.Exit(1)
	}
	fmt.Println("destroyed")
}

func imagesList() {
	data, status := apiRequest("GET", "/images", nil)
	if status != 200 {
		fmt.Fprintf(os.Stderr, "error (%d): %s\n", status, data)
		os.Exit(1)
	}
	var images []map[string]any
	json.Unmarshal(data, &images)

	if len(images) == 0 {
		fmt.Println("no images")
		return
	}
	for _, img := range images {
		size := img["size"].(float64)
		fmt.Printf("%-15s %6.0f MB\n", img["name"], size/1024/1024)
	}
}

// --- Direct DB commands ---

func createKey() {
	name := "default"
	if len(os.Args) > 2 {
		name = os.Args[2]
	}

	st, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	key := generateKey()
	ak := &store.APIKey{
		ID:        uuid.New().String(),
		Key:       key,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}

	if err := st.CreateAPIKey(context.Background(), ak); err != nil {
		fmt.Fprintf(os.Stderr, "create key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("API Key created:\n")
	fmt.Printf("  ID:   %s\n", ak.ID)
	fmt.Printf("  Name: %s\n", ak.Name)
	fmt.Printf("  Key:  %s\n", ak.Key)
	fmt.Printf("\nStore this key securely — it cannot be retrieved later.\n")
}

func generateKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "fclk_" + hex.EncodeToString(b)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: fcctl <command> [args]

Host Setup (requires root):
  setup                                Full host setup (kernel, images, bridge, service)
  build-kernel                         Build Linux 6.1 kernel for Firecracker
  build-image <name|all>               Build rootfs image (minimal/ubuntu/python/node)

API Management:
  create-key [name]                    Create a new API key (direct DB)

Sandbox Operations:
  sandbox create [--ttl N] [--image X] Create a sandbox
  sandbox list                         List active sandboxes
  sandbox exec <id> <cmd> [args...]    Execute command in sandbox
  sandbox kill <id>                    Destroy a sandbox
  images                               List available images

Shortcuts: sb = sandbox, ls = list, rm = kill

Environment:
  FCCTL_KEY       API key (required for sandbox/image commands)
  FCCTL_API       API server URL (default: http://localhost:8080)
  FCCTL_DB        SQLite path (default: /var/lib/firecrackerlacker/firecrackerlacker.db)
  FCCTL_IMAGES    Images directory (default: /opt/firecrackerlacker/images)
`)
}
