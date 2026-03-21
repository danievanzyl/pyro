// Command fcctl is the CLI for managing firecrackerlacker.
// Currently supports API key management.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
	"github.com/google/uuid"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	dbPath := os.Getenv("FCCTL_DB")
	if dbPath == "" {
		dbPath = "/var/lib/firecrackerlacker/firecrackerlacker.db"
	}

	switch os.Args[1] {
	case "create-key":
		createKey(dbPath)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func createKey(dbPath string) {
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

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: fcctl <command> [args]

Commands:
  create-key [name]  Create a new API key

Environment:
  FCCTL_DB  Path to SQLite database (default: /var/lib/firecrackerlacker/firecrackerlacker.db)
`)
}
