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
	svcpkg "github.com/klaudio-ai/klaudio/internal/service"
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

	// Handle service subcommands: klaudio service install|uninstall|start|stop|status
	if svcpkg.HandleCLI(os.Args) {
		return
	}

	// Configure structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// If running as a Windows service, use service logging (file + event log)
	if svcpkg.IsRunningAsService() {
		svcpkg.SetupServiceLogging()
	}

	slog.Info("starting klaudio", "version", version)

	// Check Docker availability early
	if err := svcpkg.CheckDockerAvailability(); err != nil {
		slog.Warn("Docker check failed — tasks will fail until Docker is available", "error", err)
	}

	// If started by the OS service manager, run in service mode
	if svcpkg.IsRunningAsService() {
		slog.Info("running as system service")
		if err := svcpkg.RunAsService(runServer); err != nil {
			slog.Error("service error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Normal foreground mode (also handles "klaudio run" subcommand)
	if err := runForeground(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

// runForeground runs the server in the foreground with signal handling.
func runForeground() error {
	stop := make(chan struct{})

	// Start the server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServer(stop)
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("received shutdown signal", "signal", sig)
		close(stop)
		// Wait for server to finish
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// runServer is the core server function used by both foreground and service modes.
// It blocks until the stop channel is closed or a fatal error occurs.
func runServer(stop <-chan struct{}) error {
	// Determine config file path
	cfgPath := resolveConfigPath()

	// Load configuration
	slog.Info("loading configuration", "path", cfgPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// If running as service, override paths to platform-appropriate locations
	if svcpkg.IsRunningAsService() {
		applyServicePaths(cfg)
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

	// Wait for stop signal or server error
	select {
	case <-stop:
		slog.Info("shutting down server...")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown
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

// resolveConfigPath determines the config file path from env/args.
func resolveConfigPath() string {
	cfgPath := "config.yaml"
	if v := os.Getenv("KLAUDIO_CONFIG"); v != "" {
		cfgPath = v
	}

	// Skip "run" subcommand when looking for config path argument
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	if len(args) > 0 && args[0] != "service" && args[0] != "--version" && args[0] != "-v" {
		cfgPath = args[0]
	}

	// If running as service, use platform config path as default
	if svcpkg.IsRunningAsService() && os.Getenv("KLAUDIO_CONFIG") == "" {
		cfgPath = svcpkg.ConfigFilePath()
	}

	return cfgPath
}

// applyServicePaths overrides config paths with platform-appropriate service paths,
// but only for values that still have the defaults.
func applyServicePaths(cfg *config.Config) {
	if cfg.Database.Path == "data/klaudio.db" {
		cfg.Database.Path = svcpkg.DatabasePath()
	}
	if cfg.Storage.DataDir == "data" {
		cfg.Storage.DataDir = svcpkg.DataDir()
	}
	if cfg.Storage.StatesDir == "data/states" {
		cfg.Storage.StatesDir = svcpkg.StatesDir()
	}
	if cfg.Storage.FilesDir == "data/files" {
		cfg.Storage.FilesDir = svcpkg.FilesDir()
	}
}
