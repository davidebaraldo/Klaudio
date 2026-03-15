package service

import (
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns the platform-appropriate directory for configuration files.
//
//	Windows: %ProgramData%\Klaudio
//	Linux:   /etc/klaudio
func ConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Klaudio")
	default:
		return "/etc/klaudio"
	}
}

// DataDir returns the platform-appropriate directory for runtime data.
//
//	Windows: %ProgramData%\Klaudio\data
//	Linux:   /var/lib/klaudio
func DataDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(ConfigDir(), "data")
	default:
		return "/var/lib/klaudio"
	}
}

// LogDir returns the platform-appropriate directory for log files.
//
//	Windows: %ProgramData%\Klaudio\logs
//	Linux:   /var/log/klaudio
func LogDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(ConfigDir(), "logs")
	default:
		return "/var/log/klaudio"
	}
}

// ConfigFilePath returns the path to the service config file.
func ConfigFilePath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// DatabasePath returns the path to the service database.
func DatabasePath() string {
	return filepath.Join(DataDir(), "klaudio.db")
}

// StatesDir returns the path to the states directory.
func StatesDir() string {
	return filepath.Join(DataDir(), "states")
}

// FilesDir returns the path to the files directory.
func FilesDir() string {
	return filepath.Join(DataDir(), "files")
}
