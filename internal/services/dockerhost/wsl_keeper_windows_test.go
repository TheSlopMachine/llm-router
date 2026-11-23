package dockerhost

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

// TestWSLKeeperHelperProcess stands in for wsl.exe: it blocks until killed.
func TestWSLKeeperHelperProcess(t *testing.T) {
	if os.Getenv("LLM_ROUTER_WSL_KEEPER_HELPER") != "1" {
		return
	}
	time.Sleep(time.Hour)
}

// TestWSLKeeperFollowsConnection guards the keeper: it holds a process for
// the stored distro, switches it with the connection, refuses a distro
// without the WSL endpoint and stops on Close.
func TestWSLKeeperFollowsConnection(t *testing.T) {
	started := make(chan string, 4)
	original := wslKeeperCommand
	t.Cleanup(func() { wslKeeperCommand = original })
	wslKeeperCommand = func(ctx context.Context, name string) *exec.Cmd {
		started <- name
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWSLKeeperHelperProcess$")
		command.Env = append(os.Environ(), "LLM_ROUTER_WSL_KEEPER_HELPER=1")
		return command
	}
	expect := func(name string) {
		t.Helper()
		select {
		case got := <-started:
			if got != name {
				t.Fatalf("kept %q, want %q", got, name)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("no keeper process for %q", name)
		}
	}

	stopped := func(service *Service) bool {
		service.wslKeeper.mu.Lock()
		defer service.wslKeeper.mu.Unlock()
		return service.wslKeeper.cancel == nil
	}

	ctx := context.Background()
	database := testutil.SetupTestSQLite(t)
	service := New(database)
	if _, err := service.PutConnection(ctx, ConnectionUpdate{Endpoint: "ssh://user@host", WSLDistribution: "Ubuntu"}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("distro without the WSL endpoint: %v", err)
	}
	if _, err := service.PutConnection(ctx, ConnectionUpdate{Endpoint: wslDockerEndpoint, WSLDistribution: "Ubuntu"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	expect("Ubuntu")
	if _, err := service.PutConnection(ctx, ConnectionUpdate{Endpoint: wslDockerEndpoint, WSLDistribution: "Debian"}); err != nil {
		t.Fatalf("switch: %v", err)
	}
	expect("Debian")
	service.Close()
	if !stopped(service) {
		t.Fatal("Close keeps the distro running")
	}

	restarted := New(database)
	if err := restarted.ResumeConnection(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
	expect("Debian")
	if _, err := restarted.PutConnection(ctx, ConnectionUpdate{}); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if !stopped(restarted) {
		t.Fatal("disconnect keeps the distro running")
	}
}
