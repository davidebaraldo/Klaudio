package main

import (
	"embed"
	"io/fs"
	"log/slog"

	"github.com/klaudio-ai/klaudio/internal/embedded"
)

// Embed the Docker build context (Dockerfile.agent + entrypoint.sh).
//
//go:embed all:docker
var dockerFS embed.FS

// Embed the built SvelteKit frontend.
// The frontend/ directory is populated by "make frontend" before "go build".
// If empty, the binary still compiles but frontend serving is disabled.
//
//go:embed all:frontend
var frontendFS embed.FS

func init() {
	// Register Docker build context files
	registerDockerFiles()

	// Register frontend filesystem
	registerFrontend()
}

func registerDockerFiles() {
	var files []embedded.DockerFile

	entries, err := fs.ReadDir(dockerFS, "docker")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, readErr := dockerFS.ReadFile("docker/" + entry.Name())
		if readErr != nil {
			slog.Warn("failed to read embedded docker file", "name", entry.Name(), "error", readErr)
			continue
		}
		files = append(files, embedded.DockerFile{
			Name: entry.Name(),
			Data: data,
		})
	}

	embedded.RegisterDockerFiles(files)
}

func registerFrontend() {
	sub, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return
	}
	embedded.RegisterFrontend(sub)
}
