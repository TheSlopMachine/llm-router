// Package systemservice controls the system service that a package install
// defines for llm-router: a systemd unit, an OpenRC script or a FreeBSD
// rc.d script. The packages come from TheSlopMachine/llm-packages, which
// owns the service names and files.
package systemservice

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Sentinel errors for service control. Match with errors.Is.
var (
	// ErrNoService reports that no package installed a service.
	ErrNoService = errors.New("no system service is installed")
	// ErrManagerNotRunning matches ManagerNotRunningError.
	ErrManagerNotRunning = errors.New("the service manager does not run")
	// ErrDeclarative reports a system whose configuration owns the
	// service state: NixOS.
	ErrDeclarative = errors.New("the system configuration owns the service state")
)

// ManagerNotRunningError reports a service file whose service manager does
// not run, as in a container.
type ManagerNotRunningError struct {
	Kind Kind
}

func (err *ManagerNotRunningError) Error() string {
	return string(err.Kind) + " does not run"
}

func (err *ManagerNotRunningError) Unwrap() error { return ErrManagerNotRunning }

// Kind names a service manager.
type Kind string

const (
	KindSystemd Kind = "systemd"
	KindOpenRC  Kind = "OpenRC"
	KindRC      Kind = "rc.d"
)

const (
	systemdUnit   = "llm-router.service"
	openRCService = "llm-router"
	rcService     = "llm_router"
)

// Manager controls the service through the tools of its service manager.
// Commands print their own output to the terminal.
type Manager struct {
	Kind Kind
	// root is the file system root: "/" outside tests.
	root string
	// declarative marks NixOS: enable and disable belong to its
	// configuration.
	declarative bool
	// run executes a command and fails on a non-zero exit.
	run func(name string, arguments ...string) error
	// succeeds executes a command and reports whether it exited with 0.
	succeeds func(name string, arguments ...string) (bool, error)
}

// Detect returns the manager of the installed service. It returns
// ErrNoService when no package installed one.
func Detect() (*Manager, error) {
	return detect(string(filepath.Separator), runtime.GOOS)
}

func detect(root, goos string) (*Manager, error) {
	exists := func(path string) (bool, error) {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil {
			return true, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	manager := &Manager{root: root, run: runCommand, succeeds: commandSucceeds}
	var serviceFile, runningMarker string
	switch goos {
	case "linux":
		for _, directory := range []string{"/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"} {
			found, err := exists(directory + "/" + systemdUnit)
			if err != nil {
				return nil, err
			}
			if found {
				manager.Kind, serviceFile, runningMarker = KindSystemd, directory+"/"+systemdUnit, "/run/systemd/system"
				break
			}
		}
		if manager.Kind == "" {
			manager.Kind, serviceFile, runningMarker = KindOpenRC, "/etc/init.d/"+openRCService, "/run/openrc"
		}
		declarative, err := exists("/etc/NIXOS")
		if err != nil {
			return nil, err
		}
		manager.declarative = declarative
	case "freebsd":
		// rc.d runs whenever FreeBSD runs.
		manager.Kind, serviceFile = KindRC, "/usr/local/etc/rc.d/"+rcService
	default:
		return nil, ErrNoService
	}
	found, err := exists(serviceFile)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNoService
	}
	if runningMarker != "" {
		running, err := exists(runningMarker)
		if err != nil {
			return nil, err
		}
		if !running {
			return nil, &ManagerNotRunningError{Kind: manager.Kind}
		}
	}
	return manager, nil
}

// Start starts the service now. It does not change the start at boot.
func (manager *Manager) Start() error {
	switch manager.Kind {
	case KindSystemd:
		return manager.run("systemctl", "start", systemdUnit)
	case KindOpenRC:
		return manager.run("rc-service", openRCService, "start")
	default:
		// "start" refuses a service that is not enabled; "onestart" does not.
		return manager.run("service", rcService, "onestart")
	}
}

// Stop stops the service and waits for the process to exit.
func (manager *Manager) Stop() error {
	switch manager.Kind {
	case KindSystemd:
		return manager.run("systemctl", "stop", systemdUnit)
	case KindOpenRC:
		return manager.run("rc-service", openRCService, "stop")
	default:
		return manager.run("service", rcService, "onestop")
	}
}

// Restart stops the service if it runs, then starts it.
func (manager *Manager) Restart() error {
	switch manager.Kind {
	case KindSystemd:
		return manager.run("systemctl", "restart", systemdUnit)
	case KindOpenRC:
		return manager.run("rc-service", openRCService, "restart")
	default:
		return manager.run("service", rcService, "onerestart")
	}
}

// Enable makes the service start at boot.
func (manager *Manager) Enable() error {
	if manager.declarative {
		return ErrDeclarative
	}
	switch manager.Kind {
	case KindSystemd:
		return manager.run("systemctl", "enable", systemdUnit)
	case KindOpenRC:
		return manager.run("rc-update", "add", openRCService, "default")
	default:
		return manager.run("service", rcService, "enable")
	}
}

// Disable stops the start at boot. A running service keeps running.
func (manager *Manager) Disable() error {
	if manager.declarative {
		return ErrDeclarative
	}
	switch manager.Kind {
	case KindSystemd:
		return manager.run("systemctl", "disable", systemdUnit)
	case KindOpenRC:
		return manager.run("rc-update", "del", openRCService, "default")
	default:
		return manager.run("service", rcService, "disable")
	}
}

// Enabled reports whether the service starts at boot.
func (manager *Manager) Enabled() (bool, error) {
	switch manager.Kind {
	case KindSystemd:
		return manager.succeeds("systemctl", "is-enabled", "--quiet", systemdUnit)
	case KindOpenRC:
		_, err := os.Stat(filepath.Join(manager.root, "etc", "runlevels", "default", openRCService))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	default:
		return manager.succeeds("service", rcService, "enabled")
	}
}

// Active reports whether the service runs or starts. A service that failed
// and waits for its next restart does not count.
func (manager *Manager) Active() (bool, error) {
	switch manager.Kind {
	case KindSystemd:
		return manager.succeeds("systemctl", "is-active", "--quiet", systemdUnit)
	case KindOpenRC:
		return manager.succeeds("rc-service", "--quiet", openRCService, "status")
	default:
		return manager.succeeds("service", rcService, "onestatus")
	}
}

// LogHint tells where the service writes its output.
func (manager *Manager) LogHint() string {
	switch manager.Kind {
	case KindSystemd:
		return "journalctl -u " + systemdUnit
	case KindOpenRC:
		return "/var/log/llm-router/llm-router.log"
	default:
		return "/var/log/messages"
	}
}

func runCommand(name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", commandLine(name, arguments), err)
	}
	return nil
}

// commandSucceeds runs a status query. Its exit code carries the answer,
// so its output is not shown.
func commandSucceeds(name string, arguments ...string) (bool, error) {
	command := exec.Command(name, arguments...)
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit):
		return false, nil
	default:
		return false, fmt.Errorf("%s: %w", commandLine(name, arguments), err)
	}
}

func commandLine(name string, arguments []string) string {
	line := name
	for _, argument := range arguments {
		line += " " + argument
	}
	return line
}
