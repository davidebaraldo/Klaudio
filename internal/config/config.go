package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the complete application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Docker   DockerConfig   `yaml:"docker"`
	Claude   ClaudeConfig   `yaml:"claude"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
	State    StateConfig    `yaml:"state"`
}

// StateConfig holds checkpoint and auto-save settings.
type StateConfig struct {
	AutoSaveInterval time.Duration `yaml:"auto_save_interval"`
	AutoSaveEnabled  bool          `yaml:"auto_save_enabled"`
	MaxCheckpoints   int           `yaml:"max_checkpoints"`
	RetentionDays    int           `yaml:"retention_days"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

// DockerConfig holds Docker daemon and container settings.
type DockerConfig struct {
	Host             string `yaml:"host"`
	ImageName        string `yaml:"image_name"`
	Network          string `yaml:"network"`
	MaxAgents        int    `yaml:"max_agents"`
	MaxAgentsPerTask int    `yaml:"max_agents_per_task"`
}

// ClaudeConfig holds Claude Code authentication settings.
type ClaudeConfig struct {
	AuthMode     string `yaml:"auth_mode"`
	HostDir      string `yaml:"host_dir"`
	HostJsonFile string `yaml:"host_json_file"` // Path to .claude.json in home dir
	SessionKey   string `yaml:"session_key"`
}

// DatabaseConfig holds SQLite database settings.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// StorageConfig holds filesystem storage paths.
type StorageConfig struct {
	DataDir   string `yaml:"data_dir"`
	StatesDir string `yaml:"states_dir"`
	FilesDir  string `yaml:"files_dir"`
}

// Load reads configuration from a YAML file and applies environment variable overrides.
// If the file does not exist, defaults are used.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config file %s: %w", path, err)
			}
			// File not found — proceed with defaults
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parsing config file %s: %w", path, err)
			}
		}
	}

	applyEnvOverrides(cfg)

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

// defaults returns a Config with sensible default values.
func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8080,
			Host: "0.0.0.0",
		},
		Docker: DockerConfig{
			Host:             defaultDockerHost(),
			ImageName:        "klaudio-agent",
			Network:          "klaudio-net",
			MaxAgents:        5,
			MaxAgentsPerTask: 3,
		},
		Claude: ClaudeConfig{
			AuthMode:     "host",
			HostDir:      defaultClaudeDir(),
			HostJsonFile: defaultClaudeJsonFile(),
		},
		Database: DatabaseConfig{
			Path: "data/klaudio.db",
		},
		Storage: StorageConfig{
			DataDir:   "data",
			StatesDir: "data/states",
			FilesDir:  "data/files",
		},
		State: StateConfig{
			AutoSaveInterval: 5 * time.Minute,
			AutoSaveEnabled:  true,
			MaxCheckpoints:   3,
			RetentionDays:    7,
		},
	}
}

// defaultDockerHost returns the platform-appropriate Docker socket path.
func defaultDockerHost() string {
	if runtime.GOOS == "windows" {
		return "npipe:////./pipe/docker_engine"
	}
	return "unix:///var/run/docker.sock"
}

// defaultClaudeDir returns the default path to the user's .claude directory.
func defaultClaudeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// defaultClaudeJsonFile returns the default path to the user's .claude.json file.
func defaultClaudeJsonFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// applyEnvOverrides reads environment variables and overrides config values.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("KLAUDIO_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("KLAUDIO_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		cfg.Docker.Host = v
	}
	if v := os.Getenv("KLAUDIO_DOCKER_IMAGE"); v != "" {
		cfg.Docker.ImageName = v
	}
	if v := os.Getenv("KLAUDIO_MAX_AGENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Docker.MaxAgents = n
		}
	}
	if v := os.Getenv("KLAUDIO_MAX_AGENTS_PER_TASK"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Docker.MaxAgentsPerTask = n
		}
	}
	if v := os.Getenv("CLAUDE_SESSION_KEY"); v != "" {
		cfg.Claude.SessionKey = v
	}
	if v := os.Getenv("KLAUDIO_AUTH_MODE"); v != "" {
		cfg.Claude.AuthMode = v
	}
	if v := os.Getenv("KLAUDIO_CLAUDE_DIR"); v != "" {
		cfg.Claude.HostDir = v
	}
	if v := os.Getenv("KLAUDIO_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("KLAUDIO_DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
	}
	if v := os.Getenv("KLAUDIO_STATES_DIR"); v != "" {
		cfg.Storage.StatesDir = v
	}
	if v := os.Getenv("KLAUDIO_FILES_DIR"); v != "" {
		cfg.Storage.FilesDir = v
	}
	if v := os.Getenv("KLAUDIO_AUTOSAVE_ENABLED"); v != "" {
		cfg.State.AutoSaveEnabled = v == "true" || v == "1"
	}
	if v := os.Getenv("KLAUDIO_AUTOSAVE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.State.AutoSaveInterval = d
		}
	}
	if v := os.Getenv("KLAUDIO_MAX_CHECKPOINTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.State.MaxCheckpoints = n
		}
	}
	if v := os.Getenv("KLAUDIO_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.State.RetentionDays = n
		}
	}
}

// validate checks that the configuration is usable.
func (c *Config) validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}
	if c.Docker.ImageName == "" {
		return fmt.Errorf("docker.image_name is required")
	}
	if c.Docker.MaxAgents < 1 {
		return fmt.Errorf("docker.max_agents must be at least 1")
	}
	if c.Claude.AuthMode != "host" && c.Claude.AuthMode != "env" {
		return fmt.Errorf("claude.auth_mode must be 'host' or 'env', got %q", c.Claude.AuthMode)
	}
	if c.Claude.AuthMode == "env" && c.Claude.SessionKey == "" {
		return fmt.Errorf("claude.session_key (or CLAUDE_SESSION_KEY env) is required when auth_mode is 'env'")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path is required")
	}
	return nil
}

// ListenAddr returns the address string for net.Listen (e.g. "0.0.0.0:8080").
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
