// Package telemetry bootstraps the OTel SDK for the server process.
// It initialises TracerProvider and MeterProvider, installs them as globals,
// and returns a shutdown function that flushes in-flight telemetry.
package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

// Setup initialises the OTel SDK, installs global providers, and returns a
// shutdown function. Call shutdown before process exit to flush buffered spans.
func Setup(ctx context.Context, reg *prometheus.Registry) (shutdown func(context.Context) error, err error) {
	// Resource attributes are attached to every span and metric.
	deployEnv := os.Getenv("DEPLOYMENT_ENV")
	if deployEnv == "" {
		deployEnv = "development"
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("degrees-of-separation"),
			semconv.DeploymentEnvironment(deployEnv),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	// --- Traces ---

	tracerOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	}

	// When an OTLP endpoint is configured (i.e. running in Docker with Tempo),
	// send traces there. Otherwise fall back to stdout so spans are visible
	// when running `go run` locally without the compose stack.
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		otlpExp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(otlpExp))
	} else {
		stdoutExp, err := stdouttrace.New()
		if err != nil {
			return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(stdoutExp))
	}

	tp := sdktrace.NewTracerProvider(tracerOpts...)
	otel.SetTracerProvider(tp)

	// --- Metrics (Prometheus bridge) ---

	// promexporter bridges OTel metrics to Prometheus exposition format.
	promExp, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("creating Prometheus metric exporter: %w", err)
	}

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(promExp),
	)
	otel.SetMeterProvider(mp)

	shutdown = func(ctx context.Context) error {
		tpErr := tp.Shutdown(ctx)
		mpErr := mp.Shutdown(ctx)
		if tpErr != nil {
			return tpErr
		}
		return mpErr
	}
	return shutdown, nil
}
