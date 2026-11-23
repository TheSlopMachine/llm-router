package systemservice

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createFiles(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, path := range paths {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name        string
		goos        string
		files       []string
		kind        Kind
		err         error
		declarative bool
	}{
		{name: "nothing", goos: "linux", err: ErrNoService},
		{name: "windows", goos: "windows", err: ErrNoService},
		{name: "systemd", goos: "linux", files: []string{"usr/lib/systemd/system/llm-router.service", "run/systemd/system/x"}, kind: KindSystemd},
		{name: "nixos", goos: "linux", files: []string{"etc/systemd/system/llm-router.service", "run/systemd/system/x", "etc/NIXOS"}, kind: KindSystemd, declarative: true},
		{name: "systemd not running", goos: "linux", files: []string{"usr/lib/systemd/system/llm-router.service"}, err: ErrManagerNotRunning},
		{name: "openrc", goos: "linux", files: []string{"etc/init.d/llm-router", "run/openrc/softlevel"}, kind: KindOpenRC},
		{name: "openrc not running", goos: "linux", files: []string{"etc/init.d/llm-router"}, err: ErrManagerNotRunning},
		{name: "freebsd", goos: "freebsd", files: []string{"usr/local/etc/rc.d/llm_router"}, kind: KindRC},
		{name: "freebsd without service", goos: "freebsd", err: ErrNoService},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			createFiles(t, root, test.files...)
			manager, err := detect(root, test.goos)
			if test.err != nil {
				if !errors.Is(err, test.err) {
					t.Fatalf("detect error = %v, want %v", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if manager.Kind != test.kind || manager.declarative != test.declarative {
				t.Errorf("manager = %s declarative %t, want %s declarative %t", manager.Kind, manager.declarative, test.kind, test.declarative)
			}
		})
	}
}

// recordingManager returns a manager that records commands instead of running them.
func recordingManager(kind Kind, declarative bool) (*Manager, *[]string) {
	commands := []string{}
	record := func(name string, arguments ...string) {
		commands = append(commands, strings.Join(append([]string{name}, arguments...), " "))
	}
	return &Manager{
		Kind:        kind,
		root:        os.TempDir(),
		declarative: declarative,
		run: func(name string, arguments ...string) error {
			record(name, arguments...)
			return nil
		},
		succeeds: func(name string, arguments ...string) (bool, error) {
			record(name, arguments...)
			return true, nil
		},
	}, &commands
}

func TestManagerCommands(t *testing.T) {
	want := map[Kind][]string{
		KindSystemd: {
			"systemctl start llm-router.service",
			"systemctl stop llm-router.service",
			"systemctl restart llm-router.service",
			"systemctl enable llm-router.service",
			"systemctl disable llm-router.service",
			"systemctl is-active --quiet llm-router.service",
		},
		KindOpenRC: {
			"rc-service llm-router start",
			"rc-service llm-router stop",
			"rc-service llm-router restart",
			"rc-update add llm-router default",
			"rc-update del llm-router default",
			"rc-service --quiet llm-router status",
		},
		KindRC: {
			"service llm_router onestart",
			"service llm_router onestop",
			"service llm_router onerestart",
			"service llm_router enable",
			"service llm_router disable",
			"service llm_router onestatus",
		},
	}
	for kind, expected := range want {
		manager, commands := recordingManager(kind, false)
		for _, step := range []func() error{manager.Start, manager.Stop, manager.Restart, manager.Enable, manager.Disable} {
			if err := step(); err != nil {
				t.Fatalf("%s: %v", kind, err)
			}
		}
		if _, err := manager.Active(); err != nil {
			t.Fatalf("%s: Active: %v", kind, err)
		}
		if strings.Join(*commands, "\n") != strings.Join(expected, "\n") {
			t.Errorf("%s commands:\n%s\nwant:\n%s", kind, strings.Join(*commands, "\n"), strings.Join(expected, "\n"))
		}
	}
}

func TestDeclarativeRefusesBootChanges(t *testing.T) {
	manager, commands := recordingManager(KindSystemd, true)
	if err := manager.Enable(); !errors.Is(err, ErrDeclarative) {
		t.Errorf("Enable error = %v, want ErrDeclarative", err)
	}
	if err := manager.Disable(); !errors.Is(err, ErrDeclarative) {
		t.Errorf("Disable error = %v, want ErrDeclarative", err)
	}
	if err := manager.Start(); err != nil {
		t.Errorf("Start: %v", err)
	}
	if len(*commands) != 1 {
		t.Errorf("commands = %v, want only the start", *commands)
	}
}
