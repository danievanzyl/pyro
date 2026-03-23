// Command server is the Pyro API server.
//
// Manages Firecracker microVM sandboxes for agentic workloads:
// - REST API for sandbox lifecycle (create, exec, destroy, files)
// - WebSocket streaming exec
// - TTL-based automatic cleanup
// - Startup reconciliation of orphaned VMs
// - API key authentication with per-key quotas
// - OTEL metrics (Prometheus + OTLP)
// - Audit logging
// - Base image management
package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pyro "github.com/danievanzyl/pyro"
	"github.com/danievanzyl/pyro/internal/api"
	"github.com/danievanzyl/pyro/internal/observability"
	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/danievanzyl/pyro/internal/store"
)

func main() {
	var (
		listenAddr     = flag.String("listen", ":8080", "API server listen address")
		dbPath         = flag.String("db", "/opt/pyro/pyro.db", "SQLite database path")
		stateDir       = flag.String("state-dir", "/opt/pyro/vms", "VM state directory")
		imagesDir      = flag.String("images-dir", "/opt/pyro/images", "Base images directory")
		firecrackerBin = flag.String("firecracker", "/usr/bin/firecracker", "Firecracker binary path")
		jailerBin      = flag.String("jailer", "/usr/bin/jailer", "Jailer binary path")
		kernelPath     = flag.String("kernel", "/opt/pyro/images/vmlinux", "Guest kernel path")
		rootfsPath     = flag.String("rootfs", "/opt/pyro/images/rootfs.ext4", "Default rootfs path")
		bridgeName     = flag.String("bridge", "fcbr0", "Network bridge name")
		maxSandboxes   = flag.Int("max-sandboxes", 100, "Maximum concurrent sandboxes")
		reaperInterval = flag.Duration("reaper-interval", 5*time.Second, "TTL reaper check interval")
		execTimeout    = flag.Duration("exec-timeout", 300*time.Second, "Default exec timeout")
		otlpEndpoint   = flag.String("otlp-endpoint", "", "OTLP HTTP endpoint (e.g., localhost:4318)")
		promEnabled    = flag.Bool("prometheus", true, "Enable Prometheus /metrics endpoint")
		maxPerKey      = flag.Int("max-per-key", 10, "Max concurrent sandboxes per API key")
		rateLimit      = flag.Int("rate-limit", 30, "Max sandbox creates per minute per key")
		maxVCPU        = flag.Int("max-vcpu", 4, "Max vCPUs per sandbox")
		maxMemMiB      = flag.Int("max-mem-mib", 2048, "Max memory per sandbox (MiB)")
		defaultVCPU    = flag.Int("default-vcpu", 1, "Default vCPUs per sandbox")
		defaultMemMiB  = flag.Int("default-mem-mib", 256, "Default memory per sandbox (MiB)")
		poolSize       = flag.Int("pool-size", 0, "Snapshot pool size per image (0 = disabled)")
		snapshotDir    = flag.String("snapshot-dir", "/opt/pyro/snapshots", "Snapshot pool directory")
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

	// Setup OTEL metrics.
	metrics, otelShutdown, err := observability.Setup(context.Background(), observability.Config{
		ServiceName:       "pyro",
		OTLPEndpoint:      *otlpEndpoint,
		PrometheusEnabled: *promEnabled,
	}, log)
	if err != nil {
		log.Warn("otel setup failed, metrics disabled", "err", err)
		// Non-fatal — server runs without metrics.
	}
	if otelShutdown != nil {
		defer otelShutdown(context.Background())
	}

	// Create sandbox manager.
	mgr, err := sandbox.New(sandbox.Config{
		StateDir:       *stateDir,
		FirecrackerBin: *firecrackerBin,
		JailerBin:      *jailerBin,
		KernelPath:     *kernelPath,
		DefaultRootfs:  *rootfsPath,
		ImagesDir:      *imagesDir,
		BridgeName:     *bridgeName,
		VsockAgentPort: 1024,
		ExecTimeout:    *execTimeout,
		MaxSandboxes:   *maxSandboxes,
		MaxVCPU:        *maxVCPU,
		MaxMemMiB:      *maxMemMiB,
		DefaultVCPU:    *defaultVCPU,
		DefaultMemMiB:  *defaultMemMiB,
		Metrics:        metrics,
	}, st, log)
	if err != nil {
		log.Error("create sandbox manager", "err", err)
		os.Exit(1)
	}

	// Create image manager.
	imgMgr, err := sandbox.NewImageManager(sandbox.ImageConfig{
		ImagesDir: *imagesDir,
	}, log)
	if err != nil {
		log.Warn("image manager init failed", "err", err)
		// Non-fatal — image API will be unavailable.
	}

	// Create quota enforcer.
	quota := api.NewQuotaEnforcer(st, api.QuotaConfig{
		MaxConcurrentSandboxes: *maxPerKey,
		MaxTTL:                 86400,
		RateLimit:              *rateLimit,
	})

	// Reconcile orphaned VMs from previous run.
	if err := mgr.Reconcile(context.Background()); err != nil {
		log.Error("reconcile failed", "err", err)
	}

	// Start TTL reaper.
	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	reaper := sandbox.NewReaper(mgr, *reaperInterval, log)
	go reaper.Run(reaperCtx)

	// Start snapshot pool (if enabled).
	var pool *sandbox.Pool
	if *poolSize > 0 {
		var err error
		pool, err = sandbox.NewPool(sandbox.PoolConfig{
			TargetSize:        *poolSize,
			SnapshotDir:       *snapshotDir,
			ReplenishInterval: 10 * time.Second,
		}, mgr, log)
		if err != nil {
			log.Error("snapshot pool init failed", "err", err)
		} else {
			mgr.SetPool(pool)
			go pool.Run(reaperCtx)
		}
	}

	// Embedded UI.
	uiFS, err := fs.Sub(pyro.UIBuild, "ui/build")
	if err != nil {
		log.Warn("ui embed not available", "err", err)
	}

	// Event bus for SSE streaming.
	eventBus := api.NewEventBus()

	// Health tick — publish active count every 5s over SSE.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reaperCtx.Done():
				return
			case <-ticker.C:
				tick := map[string]any{
					"active_sandboxes": mgr.ActiveCount(),
				}
				if pool != nil {
					tick["pool_stats"] = pool.Stats()
				}
				eventBus.Publish("health.tick", tick)
			}
		}
	}()

	// Start API server.
	srv := api.NewServer(mgr, st, log, &api.ServerConfig{
		Metrics:  metrics,
		Quota:    quota,
		ImageMgr: imgMgr,
		UIFS:     uiFS,
		EventBus: eventBus,
		Pool:     pool,
	})
	httpServer := &http.Server{
		Addr:         *listenAddr,
		Handler:      srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second, // headers only — SSE/WS need long-lived conns
		WriteTimeout:      0,                // disabled — SSE streams indefinitely
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("api server starting", "addr", *listenAddr,
			"otlp", *otlpEndpoint, "prometheus", *promEnabled,
			"max_per_key", *maxPerKey, "rate_limit", *rateLimit)
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

	log.Info("server stopped")
}
