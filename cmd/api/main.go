package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeanmamelo/medication-api/internal/config"
	"github.com/jeanmamelo/medication-api/internal/httpapi"
	"github.com/jeanmamelo/medication-api/internal/medication"
	"github.com/jeanmamelo/medication-api/internal/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.Environment)
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repository := postgres.NewRepository(pool)
	service := medication.NewService(repository, medication.RandomUUIDGenerator{})
	server := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           httpapi.NewRouterWithLogger(repository, service, logger),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	go func() {
		logger.Info("starting HTTP server", "address", cfg.HTTP.Address, "environment", cfg.Environment)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	shutdownOnSignal(server, logger, cfg.HTTP.ShutdownTimeout)
}

func newLogger(environment string) *slog.Logger {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	if environment == "dev" {
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, options))
}

func shutdownOnSignal(server *http.Server, logger *slog.Logger, timeout time.Duration) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	<-signals
	logger.Info("shutting down HTTP server")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
