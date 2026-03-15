//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
)

const systemdUnitTemplate = `[Unit]
Description={{.Description}}
Documentation=https://github.com/davidebaraldo/Klaudio
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
ExecStart={{.ExePath}} run
WorkingDirectory={{.DataDir}}
Restart=on-failure
RestartSec=10
WatchdogSec=60

# Environment
Environment=KLAUDIO_CONFIG={{.ConfigFile}}
Environment=KLAUDIO_DB_PATH={{.DatabasePath}}
Environment=KLAUDIO_DATA_DIR={{.DataDir}}
Environment=KLAUDIO_STATES_DIR={{.StatesDir}}
Environment=KLAUDIO_FILES_DIR={{.FilesDir}}

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths={{.DataDir}} {{.LogDir}}
PrivateTmp=true

# Logging (stdout → journald)
StandardOutput=journal
StandardError=journal
SyslogIdentifier={{.Name}}

# Resource limits
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`

type unitData struct {
	Name         string
	Description  string
	ExePath      string
	ConfigFile   string
	DataDir      string
	LogDir       string
	DatabasePath string
	StatesDir    string
	FilesDir     string
}

const unitFilePath = "/etc/systemd/system/klaudio.service"

// install creates the systemd unit file and enables the service.
func install() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must be run as root (use sudo)")
	}

	exePath, err := ExePath()
	if err != nil {
		return err
	}

	// Create directories
	for _, dir := range []string{ConfigDir(), DataDir(), LogDir(), StatesDir(), FilesDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Generate unit file
	tmpl, err := template.New("unit").Parse(systemdUnitTemplate)
	if err != nil {
		return fmt.Errorf("parsing unit template: %w", err)
	}

	data := unitData{
		Name:         ServiceName,
		Description:  ServiceDescription,
		ExePath:      exePath,
		ConfigFile:   ConfigFilePath(),
		DataDir:      DataDir(),
		LogDir:       LogDir(),
		DatabasePath: DatabasePath(),
		StatesDir:    StatesDir(),
		FilesDir:     FilesDir(),
	}

	f, err := os.Create(unitFilePath)
	if err != nil {
		return fmt.Errorf("creating unit file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}

	// Reload systemd and enable
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("systemd daemon-reload: %w", err)
	}
	if err := systemctl("enable", ServiceName); err != nil {
		return fmt.Errorf("enabling service: %w", err)
	}

	fmt.Printf("Service %q installed successfully.\n", ServiceName)
	fmt.Printf("  Unit file: %s\n", unitFilePath)
	fmt.Printf("  Config:    %s\n", ConfigFilePath())
	fmt.Printf("  Data:      %s\n", DataDir())
	fmt.Printf("  Logs:      journalctl -u %s\n", ServiceName)
	fmt.Println("\nStart with: sudo klaudio service start")
	return nil
}

// uninstall disables and removes the systemd service.
func uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must be run as root (use sudo)")
	}

	// Stop if running
	_ = systemctl("stop", ServiceName)
	_ = systemctl("disable", ServiceName)

	if err := os.Remove(unitFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}

	_ = systemctl("daemon-reload")

	fmt.Printf("Service %q uninstalled successfully.\n", ServiceName)
	fmt.Println("Note: config and data directories were NOT removed.")
	return nil
}

// start starts the systemd service.
func start() error {
	if err := systemctl("start", ServiceName); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	fmt.Printf("Service %q started.\n", ServiceName)
	return nil
}

// stop stops the systemd service.
func stop() error {
	if err := systemctl("stop", ServiceName); err != nil {
		return fmt.Errorf("stopping service: %w", err)
	}
	fmt.Printf("Service %q stopped.\n", ServiceName)
	return nil
}

// status queries the systemd service status.
func status() error {
	cmd := exec.Command("systemctl", "is-active", ServiceName)
	out, err := cmd.Output()
	state := strings.TrimSpace(string(out))

	if err != nil {
		// is-active returns exit 3 for inactive/not-found
		if state == "" {
			state = "not installed"
		}
	}

	fmt.Printf("Service %q: %s\n", ServiceName, state)

	// Show more details if installed
	if state != "not installed" {
		cmd = exec.Command("systemctl", "show", ServiceName,
			"--property=MainPID,ActiveState,SubState,ExecMainStartTimestamp")
		out, _ = cmd.Output()
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				fmt.Printf("  %s\n", line)
			}
		}
	}
	return nil
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isRunningAsService checks if the process is managed by systemd.
// A simple heuristic: PID 1 is systemd and INVOCATION_ID is set.
func isRunningAsService() bool {
	return os.Getenv("INVOCATION_ID") != ""
}

// runAsService on Linux is a no-op — systemd manages the process directly.
// The stop channel is closed when SIGTERM is received (handled by the caller).
func runAsService(run RunFunc) error {
	stop := make(chan struct{})
	return run(stop)
}
