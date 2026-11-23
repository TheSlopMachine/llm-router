//go:build unix

package cmd

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

const ownerTestChildVariable = "LLM_ROUTER_OWNER_TEST_CHILD"

// The switch cannot be undone, so it runs in a child test process.
func TestRunAsConfigurationOwnerSwitchesToFileOwner(t *testing.T) {
	account, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("no user nobody: %v", err)
	}
	if os.Getenv(ownerTestChildVariable) != "" {
		if err := runAsConfigurationOwner(nil, nil); err != nil {
			t.Fatalf("runAsConfigurationOwner: %v", err)
		}
		if uid := strconv.Itoa(os.Geteuid()); uid != account.Uid {
			t.Fatalf("effective uid = %s, want %s", uid, account.Uid)
		}
		if gid := strconv.Itoa(os.Getegid()); gid != account.Gid {
			t.Fatalf("effective gid = %s, want %s", gid, account.Gid)
		}
		if home := os.Getenv("HOME"); home != account.HomeDir {
			t.Fatalf("HOME = %q, want %q", home, account.HomeDir)
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	uid, _ := strconv.Atoi(account.Uid)
	gid, _ := strconv.Atoi(account.Gid)
	dotenvPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(dotenvPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dotenvPath, uid, gid); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRunAsConfigurationOwnerSwitchesToFileOwner$")
	child.Env = append(os.Environ(), ownerTestChildVariable+"=1", "ENV_FILE="+dotenvPath)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
}

func TestRunAsConfigurationOwnerKeepsRootForRootFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	dotenvPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(dotenvPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV_FILE", dotenvPath)
	if err := runAsConfigurationOwner(nil, nil); err != nil {
		t.Fatalf("runAsConfigurationOwner: %v", err)
	}
	if os.Geteuid() != 0 {
		t.Fatalf("effective uid = %d, want 0", os.Geteuid())
	}
}
