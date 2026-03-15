package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/klaudio-ai/klaudio/internal/api"
	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/files"
	"github.com/klaudio-ai/klaudio/internal/stream"
	"github.com/klaudio-ai/klaudio/internal/task"
)

// version is set at build time via ldflags:
//
//	go build -ldflags "-X main.version=v0.1.0"
var version = "dev"

func main() {
	// Handle --version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("klaudio", version)
		return
	}

	// Configure structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("starting klaudio", "version", version)

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Determine config file path
	cfgPath := "config.yaml"
	if v := os.Getenv("KLAUDIO_CONFIG"); v != "" {
		cfgPath = v
	}
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	// Load configuration
	slog.Info("loading configuration", "path", cfgPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Initialize database
	slog.Info("initializing database", "path", cfg.Database.Path)
	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	// Run migrations
	slog.Info("running database migrations")
	if err := database.Migrate(context.Background(), "migrations"); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// Ensure storage directories exist
	for _, dir := range []string{cfg.Storage.DataDir, cfg.Storage.StatesDir, cfg.Storage.FilesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Initialize Docker manager
	slog.Info("connecting to Docker daemon", "host", cfg.Docker.Host)
	dockerMgr, err := docker.NewManager(cfg)
	if err != nil {
		return fmt.Errorf("initializing docker manager: %w", err)
	}
	defer dockerMgr.Close()

	// Build agent image if it doesn't exist
	slog.Info("checking agent Docker image", "image", cfg.Docker.ImageName)
	exists, err := dockerMgr.ImageExists(context.Background())
	if err != nil {
		slog.Warn("could not check for agent image", "error", err)
	} else if !exists {
		slog.Info("building agent Docker image (this may take a few minutes)...")
		if err := dockerMgr.BuildImage(context.Background(), "docker", false); err != nil {
			slog.Warn("failed to build agent image — tasks will fail until image is available", "error", err)
		} else {
			slog.Info("agent Docker image built successfully")
		}
	} else {
		slog.Info("agent Docker image already exists")
	}

	// Create StreamHub and start its event loop
	streamHub := stream.NewHub()
	go streamHub.Run()

	// Create MessageBus for real-time agent message delivery
	msgBus := stream.NewMessageBus()

	// Create TaskManager (creates its own StateStore, Pool, and Orchestrator internally)
	taskMgr := task.NewTaskManager(database, dockerMgr, cfg, streamHub, msgBus)

	// Create FileManager for file upload/download operations
	fileMgr := files.NewManager(database, dockerMgr.Client(), cfg)

	// Build HTTP router with all services populated
	services := &api.Services{
		DB:          database,
		Docker:      dockerMgr,
		Config:      cfg,
		StreamHub:   streamHub,
		MessageBus:  msgBus,
		TaskManager: taskMgr,
		FileManager: fileMgr,
		Pool:        taskMgr.Pool,
	}
	router := api.NewRouter(cfg, services)

	// Create HTTP server
	addr := cfg.ListenAddr()
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting HTTP server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("received shutdown signal", "signal", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown
	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	// Stop the stream hub
	slog.Info("stopping stream hub...")
	streamHub.Shutdown()

	slog.Info("server stopped gracefully")
	return nil
}
