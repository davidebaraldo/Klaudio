// Package service provides cross-platform system service management for Klaudio.
// It supports installing, starting, stopping, and querying service status on
// Windows (via SCM) and Linux (via systemd).
package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// ServiceName is the name used when registering the system service.
const ServiceName = "klaudio"

// ServiceDisplayName is the human-readable name shown in service managers.
const ServiceDisplayName = "Klaudio Agent Orchestrator"

// ServiceDescription is the description shown in service managers.
const ServiceDescription = "AI agent orchestrator — launches Claude Code in Docker containers"

// Status represents the current state of the service.
type Status int

const (
	StatusUnknown Status = iota
	StatusRunning
	StatusStopped
	StatusNotInstalled
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusStopped:
		return "stopped"
	case StatusNotInstalled:
		return "not installed"
	default:
		return "unknown"
	}
}

// RunFunc is the function signature for the Klaudio server main loop.
// It receives a stop channel — when it receives a value, the server should shut down.
type RunFunc func(stop <-chan struct{}) error

// HandleCLI processes service-related CLI commands.
// Returns true if a service command was handled (caller should exit),
// false if the caller should proceed with normal startup.
func HandleCLI(args []string) bool {
	if len(args) < 2 || args[1] != "service" {
		return false
	}

	if len(args) < 3 {
		printServiceUsage()
		return true
	}

	var err error
	switch args[2] {
	case "install":
		err = install()
	case "uninstall", "remove":
		err = uninstall()
	case "start":
		err = start()
	case "stop":
		err = stop()
	case "status":
		err = status()
	default:
		fmt.Fprintf(os.Stderr, "unknown service command: %s\n", args[2])
		printServiceUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	return true
}

func printServiceUsage() {
	fmt.Println("Usage: klaudio service <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  install     Install Klaudio as a system service")
	fmt.Println("  uninstall   Remove the Klaudio system service")
	fmt.Println("  start       Start the service")
	fmt.Println("  stop        Stop the service")
	fmt.Println("  status      Show service status")
}

// IsRunningAsService returns true if the process was launched by the OS service manager.
// Platform-specific implementations detect this differently.
func IsRunningAsService() bool {
	return isRunningAsService()
}

// RunAsService runs the given function as a system service.
// On Windows, this registers with the SCM. On Linux, this is a no-op
// (systemd manages the process lifecycle directly via stdout).
func RunAsService(run RunFunc) error {
	return runAsService(run)
}

// ExePath returns the absolute path to the current executable.
func ExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("determining executable path: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

// CheckDockerAvailability verifies that Docker is reachable.
// Returns nil if Docker is available, or an error with a helpful message.
func CheckDockerAvailability() error {
	return checkDocker()
}

// SetupServiceLogging configures slog to write to the appropriate log destination
// when running as a service. On Windows, this writes to a log file with rotation.
// On Linux, stdout goes to journald via systemd.
func SetupServiceLogging() {
	logDir := LogDir()
	if logDir == "" {
		// Fallback to stdout
		return
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Warn("failed to create log directory, using stdout", "dir", logDir, "error", err)
		return
	}

	logPath := filepath.Join(logDir, "klaudio.log")
	writer, err := NewRotatingWriter(logPath, 50*1024*1024, 5) // 50MB, keep 5 files
	if err != nil {
		slog.Warn("failed to open log file, using stdout", "path", logPath, "error", err)
		return
	}

	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	slog.Info("logging to file", "path", logPath, "platform", runtime.GOOS)
}
