package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/i18n"
	"github.com/TheSlopMachine/llm-router/internal/instance"
	"github.com/TheSlopMachine/llm-router/internal/systemservice"
)

// systemServiceFor returns the system service that runs the instance of
// the resolved configuration. It returns nil when the daemon commands
// manage their own process: for a user's own configuration, and for a
// system file without an installed service.
func systemServiceFor() (*systemservice.Manager, error) {
	path, err := config.ResolveDotenvPath()
	if err != nil {
		return nil, err
	}
	if !config.IsSystemDotenvPath(path) {
		return nil, nil
	}
	manager, err := systemservice.Detect()
	if errors.Is(err, systemservice.ErrNoService) {
		return nil, nil
	}
	return manager, err
}

// requireSystemService returns the service for commands that exist only
// with one: enable and disable.
func requireSystemService() (*systemservice.Manager, error) {
	manager, err := systemServiceFor()
	var notRunning *systemservice.ManagerNotRunningError
	switch {
	case errors.As(err, &notRunning):
		return nil, errors.New(i18n.T("daemon.managerDown", notRunning.Kind))
	case err != nil:
		return nil, err
	case manager == nil:
		path, err := config.ResolveDotenvPath()
		if err != nil {
			return nil, err
		}
		return nil, errors.New(i18n.T("daemon.noService", path))
	}
	return manager, nil
}

// prepareDaemon replaces the root hook for the daemon commands. A service
// manager needs root, so root stays root for a package service. Without
// one the commands run as the owner of the configuration, like every
// other command.
func prepareDaemon(command *cobra.Command, arguments []string) error {
	manager, err := systemServiceFor()
	var notRunning *systemservice.ManagerNotRunningError
	if errors.As(err, &notRunning) {
		return errors.New(i18n.T("daemon.managerDown", notRunning.Kind))
	}
	if err != nil {
		return err
	}
	if manager != nil {
		return nil
	}
	return runAsConfigurationOwner(command, arguments)
}

// runServiceAction runs a start or restart through the service manager,
// then waits for the server (awaitService).
func runServiceAction(manager *systemservice.Manager, action func() error) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	if err := action(); err != nil {
		return err
	}
	return awaitService(manager, applicationConfig)
}

// awaitService waits for the ready mark of the server in the lock record.
// A service that stops being active failed to start. A service still
// starting after daemonReadyTimeout counts as started.
func awaitService(manager *systemservice.Manager, applicationConfig *config.Config) error {
	lockPath := lockPathFor(applicationConfig)
	began := time.Now()
	noticed := false
	for {
		info, held, err := instance.Probe(lockPath)
		if err != nil {
			return err
		}
		if held && info.Ready {
			fmt.Println(i18n.T("daemon.serviceStarted", manager.Kind, info.PID, applicationConfig.Addr))
			return nil
		}
		active, err := manager.Active()
		if err != nil {
			return err
		}
		if !active {
			return errors.New(i18n.T("daemon.serviceFailed", manager.Kind, manager.LogHint()))
		}
		elapsed := time.Since(began)
		if elapsed >= daemonReadyTimeout {
			fmt.Println(i18n.T("daemon.serviceStarting", manager.Kind, manager.LogHint()))
			return nil
		}
		if !noticed && elapsed >= daemonNoticeDelay {
			noticed = true
			fmt.Println(i18n.T("daemon.serviceWaiting", manager.Kind))
		}
		time.Sleep(5 * daemonPollInterval)
	}
}

// stopService stops the service when it runs.
func stopService(manager *systemservice.Manager) error {
	active, err := manager.Active()
	if err != nil {
		return err
	}
	if !active {
		fmt.Println(i18n.T("daemon.notRunning"))
		return nil
	}
	if err := manager.Stop(); err != nil {
		return err
	}
	fmt.Println(i18n.T("daemon.serviceStopped", manager.Kind))
	return nil
}

func runDaemonEnable(now bool) error {
	manager, err := requireSystemService()
	if err != nil {
		return err
	}
	if err := manager.Enable(); errors.Is(err, systemservice.ErrDeclarative) {
		return errors.New(i18n.T("daemon.declarative"))
	} else if err != nil {
		return err
	}
	fmt.Println(i18n.T("daemon.enabled", manager.Kind))
	if !now {
		return nil
	}
	return runServiceAction(manager, manager.Start)
}

func runDaemonDisable(now bool) error {
	manager, err := requireSystemService()
	if err != nil {
		return err
	}
	if err := manager.Disable(); errors.Is(err, systemservice.ErrDeclarative) {
		return errors.New(i18n.T("daemon.declarative"))
	} else if err != nil {
		return err
	}
	fmt.Println(i18n.T("daemon.disabled", manager.Kind))
	if !now {
		return nil
	}
	return stopService(manager)
}

// serviceStatus returns the status line of the package service, or empty
// when the instance has no service.
func serviceStatus() (string, error) {
	manager, err := systemServiceFor()
	var notRunning *systemservice.ManagerNotRunningError
	switch {
	case errors.As(err, &notRunning):
		return i18n.T("status.value.managerDown", notRunning.Kind), nil
	case err != nil:
		return "", err
	case manager == nil:
		return "", nil
	}
	enabled, err := manager.Enabled()
	if err != nil {
		return "", err
	}
	if enabled {
		return i18n.T("status.value.serviceEnabled", manager.Kind), nil
	}
	return i18n.T("status.value.serviceDisabled", manager.Kind), nil
}
