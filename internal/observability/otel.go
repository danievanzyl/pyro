// Package observability provides OTEL metrics and tracing for firecrackerlacker.
//
// Metrics exported:
//
//	fclk_sandboxes_active          gauge    — currently running sandboxes
//	fclk_sandboxes_created_total   counter  — total sandboxes created
//	fclk_sandboxes_destroyed_total counter  — total sandboxes destroyed (by reason)
//	fclk_sandbox_create_duration   histogram — time to create a sandbox
//	fclk_sandbox_exec_duration     histogram — time to execute a command
//	fclk_sandbox_ttl_remaining     histogram — TTL remaining at destruction
//	fclk_pool_available            gauge    — warm snapshots available per image
//	fclk_pool_replenish_total      counter  — pool replenishment events
//	fclk_api_requests_total        counter  — HTTP requests by method+path+status
//	fclk_api_request_duration      histogram — HTTP request latency
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Config holds OTEL configuration.
type Config struct {
	// ServiceName identifies this instance.
	ServiceName string

	// OTLPEndpoint is the OTLP HTTP endpoint (e.g., "localhost:4318").
	// If empty, only Prometheus exporter is used.
	OTLPEndpoint string

	// PrometheusEnabled exposes /metrics for Prometheus scraping.
	PrometheusEnabled bool
}

// Metrics holds all application metrics instruments.
type Metrics struct {
	SandboxesActive      otelmetric.Int64UpDownCounter
	SandboxesCreated     otelmetric.Int64Counter
	SandboxesDestroyed   otelmetric.Int64Counter
	SandboxCreateDur     otelmetric.Float64Histogram
	SandboxCreatePhase   otelmetric.Float64Histogram // phase=rootfs_copy|spawn|agent_wait
	SandboxCreateFailed  otelmetric.Int64Counter
	SandboxExecDur       otelmetric.Float64Histogram
	SandboxTTLRemaining  otelmetric.Float64Histogram
	PoolAvailable        otelmetric.Int64UpDownCounter
	PoolReplenish        otelmetric.Int64Counter
	APIRequests          otelmetric.Int64Counter
	APIRequestDur        otelmetric.Float64Histogram
}

// Setup initializes OTEL metrics and returns a Metrics instance + shutdown func.
func Setup(ctx context.Context, cfg Config, log *slog.Logger) (*Metrics, func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("0.1.0"),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create resource: %w", err)
	}

	var readers []sdkmetric.Reader

	// Prometheus exporter.
	if cfg.PrometheusEnabled {
		promExporter, err := prometheus.New()
		if err != nil {
			return nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
		}
		readers = append(readers, promExporter)
		log.Info("prometheus metrics enabled at /metrics")
	}

	// OTLP HTTP exporter.
	if cfg.OTLPEndpoint != "" {
		otlpExporter, err := otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetrichttp.WithInsecure(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create otlp exporter: %w", err)
		}
		readers = append(readers, sdkmetric.NewPeriodicReader(otlpExporter,
			sdkmetric.WithInterval(15*time.Second),
		))
		log.Info("otlp metrics enabled", "endpoint", cfg.OTLPEndpoint)
	}

	opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, r := range readers {
		opts = append(opts, sdkmetric.WithReader(r))
	}
	provider := sdkmetric.NewMeterProvider(opts...)
	otel.SetMeterProvider(provider)

	meter := provider.Meter("firecrackerlacker")
	m, err := createMetrics(meter)
	if err != nil {
		return nil, nil, fmt.Errorf("create metrics: %w", err)
	}

	shutdown := func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	}

	return m, shutdown, nil
}

func createMetrics(meter otelmetric.Meter) (*Metrics, error) {
	m := &Metrics{}
	var err error

	m.SandboxesActive, err = meter.Int64UpDownCounter("fclk_sandboxes_active",
		otelmetric.WithDescription("Currently running sandboxes"))
	if err != nil {
		return nil, err
	}

	m.SandboxesCreated, err = meter.Int64Counter("fclk_sandboxes_created_total",
		otelmetric.WithDescription("Total sandboxes created"))
	if err != nil {
		return nil, err
	}

	m.SandboxesDestroyed, err = meter.Int64Counter("fclk_sandboxes_destroyed_total",
		otelmetric.WithDescription("Total sandboxes destroyed"))
	if err != nil {
		return nil, err
	}

	m.SandboxCreateDur, err = meter.Float64Histogram("fclk_sandbox_create_duration_seconds",
		otelmetric.WithDescription("Time to create a sandbox"),
		otelmetric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10))
	if err != nil {
		return nil, err
	}

	m.SandboxCreatePhase, err = meter.Float64Histogram("fclk_sandbox_create_phase_seconds",
		otelmetric.WithDescription("Time per phase of sandbox creation (rootfs_copy, spawn, agent_wait)"),
		otelmetric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20))
	if err != nil {
		return nil, err
	}

	m.SandboxCreateFailed, err = meter.Int64Counter("fclk_sandbox_create_failed_total",
		otelmetric.WithDescription("Total failed sandbox creations"))
	if err != nil {
		return nil, err
	}

	m.SandboxExecDur, err = meter.Float64Histogram("fclk_sandbox_exec_duration_seconds",
		otelmetric.WithDescription("Time to execute a command in a sandbox"),
		otelmetric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 5, 30, 60, 300))
	if err != nil {
		return nil, err
	}

	m.SandboxTTLRemaining, err = meter.Float64Histogram("fclk_sandbox_ttl_remaining_seconds",
		otelmetric.WithDescription("TTL remaining when sandbox is destroyed"),
		otelmetric.WithExplicitBucketBoundaries(0, 10, 60, 300, 900, 3600))
	if err != nil {
		return nil, err
	}

	m.PoolAvailable, err = meter.Int64UpDownCounter("fclk_pool_available",
		otelmetric.WithDescription("Warm snapshots available in pool"))
	if err != nil {
		return nil, err
	}

	m.PoolReplenish, err = meter.Int64Counter("fclk_pool_replenish_total",
		otelmetric.WithDescription("Pool replenishment events"))
	if err != nil {
		return nil, err
	}

	m.APIRequests, err = meter.Int64Counter("fclk_api_requests_total",
		otelmetric.WithDescription("HTTP API requests"))
	if err != nil {
		return nil, err
	}

	m.APIRequestDur, err = meter.Float64Histogram("fclk_api_request_duration_seconds",
		otelmetric.WithDescription("HTTP API request duration"),
		otelmetric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5))
	if err != nil {
		return nil, err
	}

	return m, nil
}

// RecordCreatePhase records timing for a specific sandbox creation phase.
func (m *Metrics) RecordCreatePhase(ctx context.Context, image, phase string, duration time.Duration) {
	m.SandboxCreatePhase.Record(ctx, duration.Seconds(), otelmetric.WithAttributes(
		attribute.String("image", image),
		attribute.String("phase", phase),
	))
}

// RecordCreateFailed records a failed sandbox creation.
func (m *Metrics) RecordCreateFailed(ctx context.Context, image, reason string) {
	m.SandboxCreateFailed.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("image", image),
		attribute.String("reason", reason),
	))
}

// RecordSandboxCreated records a sandbox creation event.
func (m *Metrics) RecordSandboxCreated(ctx context.Context, image string, duration time.Duration) {
	m.SandboxesActive.Add(ctx, 1)
	m.SandboxesCreated.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("image", image),
	))
	m.SandboxCreateDur.Record(ctx, duration.Seconds(), otelmetric.WithAttributes(
		attribute.String("image", image),
	))
}

// RecordSandboxDestroyed records a sandbox destruction event.
func (m *Metrics) RecordSandboxDestroyed(ctx context.Context, reason string, ttlRemaining time.Duration) {
	m.SandboxesActive.Add(ctx, -1)
	m.SandboxesDestroyed.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("reason", reason),
	))
	m.SandboxTTLRemaining.Record(ctx, ttlRemaining.Seconds())
}

// RecordExec records a command execution.
func (m *Metrics) RecordExec(ctx context.Context, duration time.Duration, exitCode int) {
	m.SandboxExecDur.Record(ctx, duration.Seconds(), otelmetric.WithAttributes(
		attribute.Int("exit_code", exitCode),
	))
}

// RecordAPIRequest records an HTTP request.
func (m *Metrics) RecordAPIRequest(ctx context.Context, method, path string, status int, duration time.Duration) {
	attrs := otelmetric.WithAttributes(
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.Int("status", status),
	)
	m.APIRequests.Add(ctx, 1, attrs)
	m.APIRequestDur.Record(ctx, duration.Seconds(), attrs)
}
