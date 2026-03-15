//go:build !windows && !linux

package service

import (
	"fmt"
	"runtime"
)

// Stub implementations for unsupported platforms (macOS, FreeBSD, etc.)

func install() error {
	return fmt.Errorf("service install is not supported on %s", runtime.GOOS)
}

func uninstall() error {
	return fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
}

func start() error {
	return fmt.Errorf("service start is not supported on %s", runtime.GOOS)
}

func stop() error {
	return fmt.Errorf("service stop is not supported on %s", runtime.GOOS)
}

func status() error {
	fmt.Printf("Service management is not supported on %s.\n", runtime.GOOS)
	fmt.Println("Run klaudio directly: klaudio run")
	return nil
}

func isRunningAsService() bool {
	return false
}

func runAsService(run RunFunc) error {
	return fmt.Errorf("service mode is not supported on %s — run klaudio directly", runtime.GOOS)
}
