package files

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	gogit "github.com/go-git/go-git/v5"
	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
)

// Manager handles file operations for tasks: upload, download, and
// copying files to/from Docker containers.
type Manager struct {
	db           *db.DB
	dockerClient *client.Client
	cfg          *config.Config
}

// NewManager creates a new file Manager.
func NewManager(database *db.DB, dockerClient *client.Client, cfg *config.Config) *Manager {
	return &Manager{
		db:           database,
		dockerClient: dockerClient,
		cfg:          cfg,
	}
}

// FileInfo describes an uploaded or output file.
type FileInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Path       string    `json:"path,omitempty"`
	UploadedAt time.Time `json:"uploaded_at,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

// Upload saves a file to the task's input directory and records it in the database.
func (m *Manager) Upload(ctx context.Context, taskID, filename string, content io.Reader) (*FileInfo, error) {
	inputDir := filepath.Join(m.cfg.Storage.FilesDir, taskID, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating input directory: %w", err)
	}

	destPath := filepath.Join(inputDir, filename)
	f, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("creating file %s: %w", destPath, err)
	}
	defer f.Close()

	written, err := io.Copy(f, content)
	if err != nil {
		return nil, fmt.Errorf("writing file %s: %w", destPath, err)
	}

	// Detect MIME type
	mimeType := mime.TypeByExtension(filepath.Ext(filename))
	var mimePtr *string
	if mimeType != "" {
		mimePtr = &mimeType
	}

	// Record in database
	now := time.Now().UTC()
	taskFile := &db.TaskFile{
		ID:        uuid.New().String(),
		TaskID:    taskID,
		Name:      filename,
		Direction: "input",
		Path:      destPath,
		Size:      &written,
		MimeType:  mimePtr,
		CreatedAt: now,
	}
	if err := m.db.CreateTaskFile(ctx, taskFile); err != nil {
		slog.Error("failed to record uploaded file", "error", err)
		// Don't fail the upload — file is already on disk
	}

	return &FileInfo{
		Name:       filename,
		Size:       written,
		Path:       filepath.Join("input", filename),
		UploadedAt: now,
	}, nil
}

// ListFiles returns all files for a task, separated by direction (input/output/workspace).
func (m *Manager) ListFiles(ctx context.Context, taskID string) (inputs []FileInfo, outputs []FileInfo, workspace []FileInfo, err error) {
	inputDir := filepath.Join(m.cfg.Storage.FilesDir, taskID, "input")
	inputs = listDir(inputDir)

	outputDir := filepath.Join(m.cfg.Storage.FilesDir, taskID, "output")
	outputs = listDir(outputDir)

	workspaceDir := filepath.Join(m.cfg.Storage.DataDir, "workspaces", taskID)
	workspace = listWorkspaceFiles(workspaceDir)

	return inputs, outputs, workspace, nil
}

// DownloadPath returns the filesystem path for a file.
func (m *Manager) DownloadPath(taskID, filename, direction string) (string, error) {
	if direction == "" {
		direction = "output"
	}
	if direction == "workspace" {
		path := filepath.Join(m.cfg.Storage.DataDir, "workspaces", taskID, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "", fmt.Errorf("file %s not found in workspace", filename)
		}
		return path, nil
	}
	path := filepath.Join(m.cfg.Storage.FilesDir, taskID, direction, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("file %s not found in %s", filename, direction)
	}
	return path, nil
}

// CopyToContainer copies a file or directory from the host to a container
// using the Docker API tar stream.
func (m *Manager) CopyToContainer(ctx context.Context, containerID, srcPath, dstPath string) error {
	tarBuf, err := createTarFromPath(srcPath)
	if err != nil {
		return fmt.Errorf("creating tar archive from %s: %w", srcPath, err)
	}

	err = m.dockerClient.CopyToContainer(ctx, containerID, dstPath, tarBuf, container.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("copying to container %s: %w", containerID, err)
	}

	return nil
}

// CopyFromContainer copies a file or directory from a container to the host
// using the Docker API tar stream.
func (m *Manager) CopyFromContainer(ctx context.Context, containerID, srcPath, dstPath string) error {
	reader, _, err := m.dockerClient.CopyFromContainer(ctx, containerID, srcPath)
	if err != nil {
		return fmt.Errorf("copying from container %s: %w", containerID, err)
	}
	defer reader.Close()

	if err := extractTarToPath(reader, dstPath); err != nil {
		return fmt.Errorf("extracting tar to %s: %w", dstPath, err)
	}

	return nil
}

// CopyInputsToContainer copies all input files for a task into the container's workspace.
func (m *Manager) CopyInputsToContainer(ctx context.Context, containerID, taskID string) error {
	inputDir := filepath.Join(m.cfg.Storage.FilesDir, taskID, "input")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		return nil // No input files
	}

	return m.CopyToContainer(ctx, containerID, inputDir, "/home/agent/workspace/input")
}

// CopyOutputsFromContainer copies output files from the container to the task's output directory.
func (m *Manager) CopyOutputsFromContainer(ctx context.Context, containerID, taskID string) error {
	outputDir := filepath.Join(m.cfg.Storage.FilesDir, taskID, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	return m.CopyFromContainer(ctx, containerID, "/home/agent/workspace/output", outputDir)
}

// listDir lists files in a directory, returning FileInfo for each.
func listDir(dir string) []FileInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}
	return files
}

// listWorkspaceFiles returns workspace files. If the workspace is a git repo,
// it only returns changed/added files (not the entire repo). Otherwise it lists
// all files recursively.
func listWorkspaceFiles(dir string) []FileInfo {
	// Check if this is a git repo
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		files, ok := listGitChangedFiles(dir)
		if ok {
			return files
		}
	}
	// Fallback: not a git repo, list everything
	return listDirRecursive(dir, dir)
}

// listGitChangedFiles uses go-git to find modified/added/untracked files.
// Returns (files, true) on success, or (nil, false) if the repo can't be read.
func listGitChangedFiles(dir string) ([]FileInfo, bool) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, false
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, false
	}

	status, err := wt.Status()
	if err != nil {
		return nil, false
	}

	var files []FileInfo
	for path, s := range status {
		// Include any file that is not clean
		if s.Worktree == gogit.Unmodified && s.Staging == gogit.Unmodified {
			continue
		}

		absPath := filepath.Join(dir, filepath.FromSlash(path))
		info, err := os.Stat(absPath)
		if err != nil {
			// Deleted file — still list it with size 0
			files = append(files, FileInfo{
				Name: path,
				Size: 0,
			})
			continue
		}
		if info.IsDir() {
			continue
		}

		files = append(files, FileInfo{
			Name:      path,
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}
	return files, true
}

// listDirRecursive lists files recursively in a directory, returning FileInfo with relative paths.
func listDirRecursive(dir, baseDir string) []FileInfo {
	var files []FileInfo
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, relErr := filepath.Rel(baseDir, path)
		if relErr != nil {
			return nil
		}
		files = append(files, FileInfo{
			Name:      filepath.ToSlash(relPath),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
		return nil
	})
	return files
}

// createTarFromPath creates a tar archive from a file or directory on the host.
func createTarFromPath(srcPath string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		err = filepath.Walk(srcPath, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			relPath, relErr := filepath.Rel(srcPath, path)
			if relErr != nil {
				return relErr
			}

			header, hErr := tar.FileInfoHeader(fi, "")
			if hErr != nil {
				return hErr
			}
			header.Name = filepath.ToSlash(relPath)

			if wErr := tw.WriteHeader(header); wErr != nil {
				return wErr
			}

			if fi.IsDir() {
				return nil
			}

			f, oErr := os.Open(path)
			if oErr != nil {
				return oErr
			}
			defer f.Close()

			_, cErr := io.Copy(tw, f)
			return cErr
		})
		if err != nil {
			tw.Close()
			return nil, err
		}
	} else {
		header, hErr := tar.FileInfoHeader(info, "")
		if hErr != nil {
			tw.Close()
			return nil, hErr
		}
		header.Name = filepath.Base(srcPath)

		if err := tw.WriteHeader(header); err != nil {
			tw.Close()
			return nil, err
		}

		f, oErr := os.Open(srcPath)
		if oErr != nil {
			tw.Close()
			return nil, oErr
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			tw.Close()
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

// extractTarToPath extracts a tar archive to a directory on the host.
func extractTarToPath(reader io.Reader, dstPath string) error {
	if err := os.MkdirAll(dstPath, 0o755); err != nil {
		return err
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dstPath, filepath.FromSlash(header.Name))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			f, fErr := os.Create(target)
			if fErr != nil {
				return fErr
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}

	return nil
}
