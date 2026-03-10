package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eleonorayaya/utena/internal/api"
)

func main() {
	cfg, err := api.LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := api.NewApp(cfg)
	if err != nil {
		slog.Error("Failed to open database", "error", err)
		os.Exit(1)
	}

	if err := app.OnStart(ctx); err != nil {
		slog.Error("Failed to initialize", "error", err)
		os.Exit(1)
	}
	defer app.OnEnd(ctx)

	server := api.BuildServer(app, cfg)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Starting daemon", "addr", cfg.Addr())
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("Shutting down HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
		}
	case err := <-serverErr:
		slog.Error("HTTP server failed", "error", err)
	}
}
