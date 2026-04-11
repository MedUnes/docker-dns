package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/medunes/docker-dns/internal/cache"
	"github.com/medunes/docker-dns/internal/config"
	"github.com/medunes/docker-dns/internal/docker"
	"github.com/medunes/docker-dns/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Use plain log here because slog isn't configured yet.
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	// Structured JSON logger for production; swap to NewTextHandler for dev.
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// DNS record cache.
	dnsCache := cache.New(cfg.TTL, cfg.MaxCacheSize)
	defer dnsCache.Stop()

	// Docker API client.
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		slog.Error("failed to create docker client", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := dockerClient.Close(); err != nil {
			slog.Warn("error closing docker client", "error", err)
		}
	}()

	// Assemble the DNS server.
	srv := server.New(cfg, dnsCache, dockerClient, logger)

	// Capture SIGINT / SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}

	slog.Info("shutdown complete")
}
