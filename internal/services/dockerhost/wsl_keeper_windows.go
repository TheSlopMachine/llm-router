package dockerhost

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Restart bounds of the keeper process. A run longer than the steady time
// resets the delay.
const (
	wslKeeperRetryMinimum = 5 * time.Second
	wslKeeperRetryMaximum = time.Minute
	wslKeeperSteadyRun    = time.Minute
)

// wslKeeperCommand builds the process that keeps a distro running. Tests
// replace it.
var wslKeeperCommand = func(ctx context.Context, name string) *exec.Cmd {
	return exec.CommandContext(ctx, "wsl.exe", "--distribution", name, "--exec", "sleep", "infinity")
}

// wslKeeper keeps one WSL distro running. WSL stops a distro without user
// processes after instanceIdleTimeout, and systemd services do not count. A
// wsl.exe child that runs sleep counts, so the distro and its Docker stay up
// while the router runs. Starting the child also boots a stopped distro.
type wslKeeper struct {
	mu           sync.Mutex
	distribution string
	cancel       context.CancelFunc
	done         chan struct{}
}

// keepWSLDistribution switches the kept distro. An empty name keeps none.
func (service *Service) keepWSLDistribution(name string) {
	keeper := &service.wslKeeper
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	if name == keeper.distribution {
		return
	}
	if keeper.cancel != nil {
		keeper.cancel()
		<-keeper.done
		keeper.cancel, keeper.done = nil, nil
		service.logInfo("WSL distro released", "distribution", keeper.distribution)
	}
	keeper.distribution = name
	if name == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	keeper.cancel, keeper.done = cancel, done
	go func() {
		defer close(done)
		service.runWSLKeeper(ctx, name)
	}()
}

// runWSLKeeper holds the keeper process until ctx ends and restarts it with
// a growing delay when it exits.
func (service *Service) runWSLKeeper(ctx context.Context, name string) {
	delay := wslKeeperRetryMinimum
	for {
		started := time.Now()
		var output bytes.Buffer
		command := wslKeeperCommand(ctx, name)
		command.Stdout = &output
		command.Stderr = &output
		err := command.Start()
		if err == nil {
			service.logInfo("WSL distro kept running", "distribution", name)
			err = command.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > wslKeeperSteadyRun {
			delay = wslKeeperRetryMinimum
		}
		service.logWarn("WSL distro keeper exited", "distribution", name, "error", fmt.Errorf("%v: %s", err, strings.TrimSpace(decodeWSLOutput(output.Bytes()))), "retry_ms", delay.Milliseconds())
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, wslKeeperRetryMaximum)
	}
}
