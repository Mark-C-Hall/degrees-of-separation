package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/graph"
	"github.com/mark-c-hall/degrees-of-separation/internal/handler"
	"github.com/mark-c-hall/degrees-of-separation/internal/telemetry"
	"github.com/mark-c-hall/degrees-of-separation/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Isolated registry — excludes Go runtime metrics from prometheus.DefaultRegisterer.
	reg := prometheus.NewRegistry()

	ctx := context.Background()
	otelShutdown, err := telemetry.Setup(ctx, reg)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}

	d, err := graph.NewDriver(ctx, *cfg)
	if err != nil {
		log.Fatalf("failed to initialize neo4j driver: %v", err)
	}

	if err := d.SetupSchema(ctx); err != nil {
		log.Fatalf("failed to set up schema: %v", err)
	}

	h, err := handler.NewHandler(d, web.FS, cfg.Server, logger, reg)
	if err != nil {
		log.Fatalf("failed to initialize handler: %v", err)
	}

	srv := http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      h,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("server listening on %s", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-sigCtx.Done()
	log.Println("shutdown signal received")

	timeoutCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(timeoutCtx); err != nil {
		log.Printf("shutdown did not complete cleanly: %v", err)
	}

	if err := otelShutdown(timeoutCtx); err != nil {
		log.Printf("OTel shutdown did not complete cleanly: %v", err)
	}

	log.Println("server stopped")
}
