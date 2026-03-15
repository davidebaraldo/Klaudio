package docker

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/klaudio-ai/klaudio/internal/config"
)

// Manager handles all Docker operations for agent containers.
type Manager struct {
	client    *client.Client
	imageName string
	network   string
	cfg       *config.Config
}

// ContainerOpts holds the parameters for creating an agent container.
type ContainerOpts struct {
	Name    string
	Prompt  string
	EnvVars map[string]string
	Volumes []VolumeMount
	WorkDir string
}

// VolumeMount describes a host-to-container bind mount.
type VolumeMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// NewManager creates a Docker manager connected to the daemon specified in the config.
func NewManager(cfg *config.Config) (*Manager, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}
	if cfg.Docker.Host != "" {
		opts = append(opts, client.WithHost(cfg.Docker.Host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	// Verify the daemon is reachable
	_, err = cli.Ping(context.Background())
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("pinging docker daemon at %s: %w", cfg.Docker.Host, err)
	}

	return &Manager{
		client:    cli,
		imageName: cfg.Docker.ImageName,
		network:   cfg.Docker.Network,
		cfg:       cfg,
	}, nil
}

// Client returns the underlying Docker SDK client.
// This is used by packages (e.g. files) that need direct Docker API access.
func (m *Manager) Client() *client.Client {
	return m.client
}

// Close releases the Docker client resources.
func (m *Manager) Close() error {
	return m.client.Close()
}

// CreateContainer creates a new container with the given options but does not start it.
// Returns the container ID.
func (m *Manager) CreateContainer(ctx context.Context, opts ContainerOpts) (string, error) {
	env := buildEnvList(opts.EnvVars)

	// Add Claude prompt to env
	if opts.Prompt != "" {
		env = append(env, "CLAUDE_PROMPT="+opts.Prompt)
	}

	// Add Claude auth based on config
	switch m.cfg.Claude.AuthMode {
	case "env":
		if m.cfg.Claude.SessionKey != "" {
			env = append(env, "CLAUDE_CODE_MAX_SESSION_KEY="+m.cfg.Claude.SessionKey)
		}
	}

	// Build bind mounts
	binds := make([]string, 0, len(opts.Volumes))
	for _, v := range opts.Volumes {
		bind := v.HostPath + ":" + v.ContainerPath
		if v.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	// Add Claude auth file mounts if auth_mode is "host"
	// Mount individual auth files (not the whole .claude/ dir which can be >1GB)
	// to staging paths, then entrypoint copies them to writable locations.
	if m.cfg.Claude.AuthMode == "host" && m.cfg.Claude.HostDir != "" {
		authFiles := []struct {
			hostPath      string
			containerPath string
		}{
			{filepath.Join(m.cfg.Claude.HostDir, ".credentials.json"), "/tmp/.claude-credentials.json"},
			{filepath.Join(m.cfg.Claude.HostDir, "settings.json"), "/tmp/.claude-settings.json"},
			{filepath.Join(m.cfg.Claude.HostDir, "settings.local.json"), "/tmp/.claude-settings-local.json"},
		}
		for _, f := range authFiles {
			if _, statErr := os.Stat(f.hostPath); statErr == nil {
				binds = append(binds, f.hostPath+":"+f.containerPath+":ro")
			}
		}
		// Mount .claude.json from home directory
		if m.cfg.Claude.HostJsonFile != "" {
			if _, statErr := os.Stat(m.cfg.Claude.HostJsonFile); statErr == nil {
				binds = append(binds, m.cfg.Claude.HostJsonFile+":/tmp/.claude.json.host:ro")
			}
		}
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "/home/agent/workspace"
	}

	containerCfg := &container.Config{
		Image:      m.imageName,
		Env:        env,
		WorkingDir: workDir,
		Tty:        true,
		OpenStdin:  true,
		StdinOnce:  false,
	}

	hostCfg := &container.HostConfig{
		Binds: binds,
	}

	networkCfg := &network.NetworkingConfig{}

	resp, err := m.client.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("creating container %s: %w", opts.Name, err)
	}

	return resp.ID, nil
}

// StartContainer starts a previously created container.
func (m *Manager) StartContainer(ctx context.Context, containerID string) error {
	if err := m.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container %s: %w", containerID, err)
	}
	return nil
}

// StopContainer stops a running container with a grace period in seconds.
func (m *Manager) StopContainer(ctx context.Context, containerID string, timeoutSec int) error {
	timeout := timeoutSec
	stopOpts := container.StopOptions{Timeout: &timeout}
	if err := m.client.ContainerStop(ctx, containerID, stopOpts); err != nil {
		return fmt.Errorf("stopping container %s: %w", containerID, err)
	}
	return nil
}

// RemoveContainer removes a container, forcefully if needed.
func (m *Manager) RemoveContainer(ctx context.Context, containerID string) error {
	if err := m.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("removing container %s: %w", containerID, err)
	}
	return nil
}

// AttachContainer attaches to a container's stdin/stdout/stderr streams.
// Returns a reader for combined stdout+stderr and a writer for stdin.
func (m *Manager) AttachContainer(ctx context.Context, containerID string) (io.Reader, io.WriteCloser, error) {
	resp, err := m.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("attaching to container %s: %w", containerID, err)
	}

	return resp.Reader, resp.Conn, nil
}

// WaitContainer waits for a container to exit. It returns channels for the
// exit status code and any error that occurred while waiting.
func (m *Manager) WaitContainer(ctx context.Context, containerID string) (<-chan int64, <-chan error) {
	statusCh, errCh := m.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	exitCh := make(chan int64, 1)
	waitErrCh := make(chan error, 1)

	go func() {
		select {
		case status := <-statusCh:
			exitCh <- status.StatusCode
			if status.Error != nil {
				waitErrCh <- fmt.Errorf("container error: %s", status.Error.Message)
			} else {
				waitErrCh <- nil
			}
		case err := <-errCh:
			exitCh <- -1
			waitErrCh <- fmt.Errorf("waiting for container %s: %w", containerID, err)
		case <-ctx.Done():
			exitCh <- -1
			waitErrCh <- ctx.Err()
		}
	}()

	return exitCh, waitErrCh
}

// ContainerLogs retrieves the logs from a container.
func (m *Manager) ContainerLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	reader, err := m.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
	})
	if err != nil {
		return nil, fmt.Errorf("getting logs for container %s: %w", containerID, err)
	}
	return reader, nil
}

// CopyFromContainer copies a file or directory from a container to the host
// using the Docker API tar stream. Works on both running and stopped containers.
func (m *Manager) CopyFromContainer(ctx context.Context, containerID, srcPath, dstPath string) error {
	reader, _, err := m.client.CopyFromContainer(ctx, containerID, srcPath)
	if err != nil {
		return fmt.Errorf("copying from container %s: %w", containerID, err)
	}
	defer reader.Close()

	if err := os.MkdirAll(dstPath, 0o755); err != nil {
		return fmt.Errorf("creating destination %s: %w", dstPath, err)
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		target := filepath.Join(dstPath, filepath.FromSlash(header.Name))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dir, err)
			}
			f, fErr := os.Create(target)
			if fErr != nil {
				return fmt.Errorf("creating file %s: %w", target, fErr)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("writing file %s: %w", target, err)
			}
			f.Close()
		}
	}

	return nil
}

// ContainerStats represents a snapshot of container resource usage.
type ContainerStats struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsage  uint64  `json:"memory_usage"`   // bytes
	MemoryLimit  uint64  `json:"memory_limit"`   // bytes
	MemoryPercent float64 `json:"memory_percent"`
	NetRx        uint64  `json:"net_rx"`          // bytes received
	NetTx        uint64  `json:"net_tx"`          // bytes sent
	BlockRead    uint64  `json:"block_read"`      // bytes read
	BlockWrite   uint64  `json:"block_write"`     // bytes written
	PIDs         uint64  `json:"pids"`
}

// GetContainerStats returns a one-shot stats snapshot for a container.
func (m *Manager) GetContainerStats(ctx context.Context, containerID string) (*ContainerStats, error) {
	resp, err := m.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, fmt.Errorf("getting container stats: %w", err)
	}
	defer resp.Body.Close()

	var raw container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding container stats: %w", err)
	}

	return parseStats(&raw), nil
}

// parseStats converts Docker's StatsJSON into our simplified ContainerStats.
func parseStats(raw *container.StatsResponse) *ContainerStats {
	s := &ContainerStats{
		MemoryUsage: raw.MemoryStats.Usage,
		MemoryLimit: raw.MemoryStats.Limit,
		PIDs:        raw.PidsStats.Current,
	}

	// Memory percent
	if s.MemoryLimit > 0 {
		s.MemoryPercent = float64(s.MemoryUsage) / float64(s.MemoryLimit) * 100
	}

	// CPU percent (delta-based)
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage - raw.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		cpuCount := float64(raw.CPUStats.OnlineCPUs)
		if cpuCount == 0 {
			cpuCount = float64(len(raw.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpuCount > 0 {
			s.CPUPercent = (cpuDelta / sysDelta) * cpuCount * 100
		}
	}

	// Network I/O (sum all interfaces)
	for _, v := range raw.Networks {
		s.NetRx += v.RxBytes
		s.NetTx += v.TxBytes
	}

	// Block I/O
	for _, bio := range raw.BlkioStats.IoServiceBytesRecursive {
		switch bio.Op {
		case "read", "Read":
			s.BlockRead += bio.Value
		case "write", "Write":
			s.BlockWrite += bio.Value
		}
	}

	return s
}

// buildEnvList converts a map of environment variables to a slice of "KEY=VALUE" strings.
func buildEnvList(vars map[string]string) []string {
	env := make([]string, 0, len(vars))
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	return env
}
