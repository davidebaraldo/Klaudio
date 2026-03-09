package state

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// CheckpointInfo provides summary information about a stored checkpoint.
type CheckpointInfo struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes"`
}

// SaveOpts holds all the data needed to create a checkpoint.
type SaveOpts struct {
	WorkspaceDir  string
	ContainerIDs  []string
	PlanProgress  db.PlanProgress
	AgentStates   []db.AgentState
	RepoState     *db.RepoState
	DockerManager *docker.Manager
	StreamHub     *stream.Hub
	TaskPrompt    string
	PlanJSON      string // raw plan JSON for resume prompt
}

// RestoreResult holds the data returned when restoring a checkpoint.
type RestoreResult struct {
	Checkpoint     *db.Checkpoint
	WorkspaceDir   string
	ClaudeMemoryDir string
	ResumePrompt   string
}

// calculateDirSize recursively calculates the total size of all files in a directory.
func calculateDirSize(dir string) (int64, error) {
	var size int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source %s: %w", src, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source %s is not a directory", src)
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("creating destination %s: %w", dst, err)
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		target := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyFile(path, target)
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}

// writeJSON serializes v as indented JSON and writes it to path.
func writeJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// readJSON reads a JSON file at path and unmarshals it into v.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshaling %s: %w", path, err)
	}
	return nil
}
