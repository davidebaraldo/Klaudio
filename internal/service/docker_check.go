package service

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// checkDocker verifies that Docker is reachable by running `docker info`.
func checkDocker() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Docker is not available: %w\n\n%s", err, dockerHelpMessage())
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		return fmt.Errorf("Docker returned empty version — daemon may not be running\n\n%s", dockerHelpMessage())
	}

	return nil
}

func dockerHelpMessage() string {
	switch runtime.GOOS {
	case "windows":
		return `Klaudio requires Docker Desktop to be running.

To fix this:
  1. Install Docker Desktop from https://docker.com/products/docker-desktop
  2. Start Docker Desktop
  3. Ensure it is running (check the system tray icon)
  4. Try again`
	case "linux":
		return `Klaudio requires Docker Engine to be installed and running.

To fix this:
  1. Install Docker: curl -fsSL https://get.docker.com | sh
  2. Start the daemon: sudo systemctl start docker
  3. Enable auto-start: sudo systemctl enable docker
  4. Add your user to the docker group: sudo usermod -aG docker $USER
  5. Try again (you may need to log out and back in)`
	default:
		return `Klaudio requires Docker to be installed and running.
Please install Docker and ensure the daemon is accessible.`
	}
}
