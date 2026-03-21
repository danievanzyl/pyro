// Command server is the firecrackerlacker API server.
//
// It manages Firecracker microVM sandboxes for agentic workloads:
// - REST API for sandbox lifecycle (create, exec, destroy)
// - TTL-based automatic cleanup
// - Startup reconciliation of orphaned VMs
// - API key authentication
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/api"
	"github.com/danievanzyl/firecrackerlacker/internal/sandbox"
	"github.com/danievanzyl/firecrackerlacker/internal/store"
)

func main() {
	var (
		listenAddr     = flag.String("listen", ":8080", "API server listen address")
		dbPath         = flag.String("db", "/var/lib/firecrackerlacker/firecrackerlacker.db", "SQLite database path")
		stateDir       = flag.String("state-dir", "/var/lib/firecrackerlacker/vms", "VM state directory")
		firecrackerBin = flag.String("firecracker", "/usr/bin/firecracker", "Firecracker binary path")
		jailerBin      = flag.String("jailer", "/usr/bin/jailer", "Jailer binary path")
		kernelPath     = flag.String("kernel", "/var/lib/firecrackerlacker/images/vmlinux", "Guest kernel path")
		rootfsPath     = flag.String("rootfs", "/var/lib/firecrackerlacker/images/rootfs.ext4", "Default rootfs path")
		bridgeName     = flag.String("bridge", "fcbr0", "Network bridge name")
		maxSandboxes   = flag.Int("max-sandboxes", 100, "Maximum concurrent sandboxes")
		reaperInterval = flag.Duration("reaper-interval", 5*time.Second, "TTL reaper check interval")
		execTimeout    = flag.Duration("exec-timeout", 300*time.Second, "Default exec timeout")
	)
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	// Open store.
	st, err := store.New(*dbPath)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	// Create sandbox manager.
	mgr, err := sandbox.New(sandbox.Config{
		StateDir:       *stateDir,
		FirecrackerBin: *firecrackerBin,
		JailerBin:      *jailerBin,
		KernelPath:     *kernelPath,
		DefaultRootfs:  *rootfsPath,
		BridgeName:     *bridgeName,
		VsockAgentPort: 1024,
		ExecTimeout:    *execTimeout,
		MaxSandboxes:   *maxSandboxes,
	}, st, log)
	if err != nil {
		log.Error("create sandbox manager", "err", err)
		os.Exit(1)
	}

	// Reconcile orphaned VMs from previous run.
	if err := mgr.Reconcile(context.Background()); err != nil {
		log.Error("reconcile failed", "err", err)
		// Non-fatal — continue startup.
	}

	// Start TTL reaper.
	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	reaper := sandbox.NewReaper(mgr, *reaperInterval, log)
	go reaper.Run(reaperCtx)

	// Start API server.
	srv := api.NewServer(mgr, st, log)
	httpServer := &http.Server{
		Addr:         *listenAddr,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second, // long for exec requests
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info("api server starting", "addr", *listenAddr)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("shutdown signal received", "signal", sig)

	// Graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	reaperCancel()
	httpServer.Shutdown(shutdownCtx)
	mgr.Shutdown(shutdownCtx)

	_ = fmt.Sprintf("") // suppress unused import
	log.Info("server stopped")
}
