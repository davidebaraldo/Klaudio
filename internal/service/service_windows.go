//go:build windows

package service

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// install registers Klaudio as a Windows service via the SCM.
func install() error {
	exePath, err := ExePath()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to Service Control Manager: %w\n(run as Administrator)", err)
	}
	defer m.Disconnect()

	// Check if already installed
	s, err := m.OpenService(ServiceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q is already installed", ServiceName)
	}

	// Create service
	cfg := mgr.Config{
		DisplayName:  ServiceDisplayName,
		Description:  ServiceDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}

	s, err = m.CreateService(ServiceName, exePath, cfg, "run")
	if err != nil {
		return fmt.Errorf("creating service: %w", err)
	}
	defer s.Close()

	// Configure recovery: restart after 10s on first two failures, 60s on subsequent
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}
	if err := s.SetRecoveryActions(recoveryActions, 86400); err != nil {
		slog.Warn("failed to set recovery actions", "error", err)
	}

	// Install event log source
	if err := eventlog.InstallAsEventCreate(ServiceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		slog.Warn("failed to install event log source", "error", err)
	}

	// Ensure config and data directories exist
	for _, dir := range []string{ConfigDir(), DataDir(), LogDir()} {
		os.MkdirAll(dir, 0o755)
	}

	fmt.Printf("Service %q installed successfully.\n", ServiceName)
	fmt.Printf("  Config:  %s\n", ConfigFilePath())
	fmt.Printf("  Data:    %s\n", DataDir())
	fmt.Printf("  Logs:    %s\n", LogDir())
	fmt.Println("\nStart with: klaudio service start")
	return nil
}

// uninstall removes the Klaudio Windows service.
func uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to SCM: %w\n(run as Administrator)", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", ServiceName)
	}
	defer s.Close()

	// Stop if running
	st, err := s.Query()
	if err == nil && st.State == svc.Running {
		s.Control(svc.Stop)
		// Wait briefly for stop
		for i := 0; i < 10; i++ {
			st, _ = s.Query()
			if st.State == svc.Stopped {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("deleting service: %w", err)
	}

	eventlog.Remove(ServiceName)

	fmt.Printf("Service %q uninstalled successfully.\n", ServiceName)
	fmt.Println("Note: config and data directories were NOT removed.")
	return nil
}

// start starts the Klaudio Windows service via SCM.
func start() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", ServiceName)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}

	fmt.Printf("Service %q started.\n", ServiceName)
	return nil
}

// stop stops the Klaudio Windows service.
func stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", ServiceName)
	}
	defer s.Close()

	st, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stopping service: %w", err)
	}

	// Wait for stop
	for i := 0; i < 20; i++ {
		if st.State == svc.Stopped {
			break
		}
		time.Sleep(500 * time.Millisecond)
		st, _ = s.Query()
	}

	if st.State != svc.Stopped {
		return fmt.Errorf("service did not stop within 10 seconds")
	}

	fmt.Printf("Service %q stopped.\n", ServiceName)
	return nil
}

// status queries the current state of the Klaudio service.
func status() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		fmt.Printf("Service %q: not installed\n", ServiceName)
		return nil
	}
	defer s.Close()

	st, err := s.Query()
	if err != nil {
		return fmt.Errorf("querying service: %w", err)
	}

	stateStr := "unknown"
	switch st.State {
	case svc.Stopped:
		stateStr = "stopped"
	case svc.StartPending:
		stateStr = "starting"
	case svc.StopPending:
		stateStr = "stopping"
	case svc.Running:
		stateStr = "running"
	case svc.Paused:
		stateStr = "paused"
	}

	fmt.Printf("Service %q: %s\n", ServiceName, stateStr)
	fmt.Printf("  PID: %d\n", st.ProcessId)
	return nil
}

// isRunningAsService detects if the process was started by the Windows SCM.
func isRunningAsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

// klaudioService implements the svc.Handler interface for the Windows SCM.
type klaudioService struct {
	run  RunFunc
	stop chan struct{}
}

// runAsService runs the Klaudio server as a Windows service.
func runAsService(run RunFunc) error {
	elog, err := eventlog.Open(ServiceName)
	if err != nil {
		// Try without event log
		elog = nil
	}
	if elog != nil {
		defer elog.Close()
		elog.Info(1, fmt.Sprintf("%s service starting", ServiceDisplayName))
	}

	s := &klaudioService{
		run:  run,
		stop: make(chan struct{}),
	}

	err = svc.Run(ServiceName, s)
	if err != nil {
		if elog != nil {
			elog.Error(1, fmt.Sprintf("service run failed: %v", err))
		}
		return fmt.Errorf("running Windows service: %w", err)
	}
	return nil
}

// Execute implements svc.Handler. It's called by the Windows SCM.
func (s *klaudioService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	changes <- svc.Status{State: svc.StartPending}

	// Start the server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.run(s.stop)
	}()

	changes <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				close(s.stop)
				// Wait for run() to finish
				if err := <-errCh; err != nil {
					slog.Error("service run error during shutdown", "error", err)
					return false, 1
				}
				return false, 0
			case svc.Interrogate:
				changes <- c.CurrentStatus
			}

		case err := <-errCh:
			if err != nil {
				slog.Error("service run error", "error", err)
				return false, 1
			}
			return false, 0
		}
	}
}
