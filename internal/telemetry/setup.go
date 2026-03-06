// Package telemetry bootstraps the OTel SDK for the server process.
// It initialises TracerProvider and MeterProvider, installs them as globals,
// and returns a shutdown function that flushes in-flight telemetry.
package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Setup initialises the OTel SDK, installs global providers, and returns a
// shutdown function. Call shutdown before process exit to flush buffered spans.
func Setup(ctx context.Context) (shutdown func(context.Context) error, err error) {
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

	// --- Metrics ---

	mpOpts := []metric.Option{metric.WithResource(res)}

	// When an OTLP endpoint is configured, push both traces and metrics via OTLP.
	// Otherwise fall back to stdout traces; metrics are collected but discarded
	// (no-reader SDK) when running locally without the compose stack.
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		otlpTraceExp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(otlpTraceExp))

		otlpMetricExp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
		}
		mpOpts = append(mpOpts, metric.WithReader(metric.NewPeriodicReader(otlpMetricExp)))
	} else {
		stdoutExp, err := stdouttrace.New()
		if err != nil {
			return nil, fmt.Errorf("creating stdout trace exporter: %w", err)
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(stdoutExp))
	}

	tp := sdktrace.NewTracerProvider(tracerOpts...)
	otel.SetTracerProvider(tp)

	mp := metric.NewMeterProvider(mpOpts...)
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
