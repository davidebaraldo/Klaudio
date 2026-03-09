package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/archive"
)

// ImageExists checks whether the agent image already exists locally.
func (m *Manager) ImageExists(ctx context.Context) (bool, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("reference", m.imageName)

	images, err := m.client.ImageList(ctx, image.ListOptions{Filters: filterArgs})
	if err != nil {
		return false, fmt.Errorf("listing images: %w", err)
	}
	return len(images) > 0, nil
}

// BuildImage builds the klaudio-agent Docker image from the docker/ directory.
// If the image already exists and force is false, it returns immediately.
func (m *Manager) BuildImage(ctx context.Context, dockerDir string, force bool) error {
	if !force {
		exists, err := m.ImageExists(ctx)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	// Resolve the absolute path to the docker build context
	absDir, err := filepath.Abs(dockerDir)
	if err != nil {
		return fmt.Errorf("resolving docker directory path: %w", err)
	}

	// Verify Dockerfile exists
	dockerfilePath := filepath.Join(absDir, "Dockerfile.agent")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile.agent not found at %s", dockerfilePath)
	}

	// Create a tar archive of the build context
	buildContext, err := archive.TarWithOptions(absDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("creating build context tar: %w", err)
	}
	defer buildContext.Close()

	resp, err := m.client.ImageBuild(ctx, buildContext, types.ImageBuildOptions{
		Dockerfile: "Dockerfile.agent",
		Tags:       []string{m.imageName},
		Remove:     true,
		ForceRemove: true,
	})
	if err != nil {
		return fmt.Errorf("building image %s: %w", m.imageName, err)
	}
	defer resp.Body.Close()

	// Read the build output to completion (required for the build to finish)
	if err := consumeBuildOutput(resp.Body); err != nil {
		return fmt.Errorf("reading build output for %s: %w", m.imageName, err)
	}

	return nil
}

// consumeBuildOutput reads and processes the Docker build output stream.
// It checks for errors in the JSON stream messages.
func consumeBuildOutput(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	for {
		var msg struct {
			Stream      string `json:"stream"`
			Error       string `json:"error"`
			ErrorDetail *struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decoding build output: %w", err)
		}
		if msg.Error != "" {
			return fmt.Errorf("build error: %s", msg.Error)
		}
	}
}
