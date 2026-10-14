package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// runLayout is the isolated directory tree of one run. Every harness reads its
// configuration and session state from here, never from the runner's home.
type runLayout struct {
	root         string
	workdir      string
	home         string
	claudeConfig string
	codexHome    string
	xdgConfig    string
	xdgData      string
	xdgCache     string
}

// newRunLayout creates the run directory tree under runRoot.
func newRunLayout(runRoot, harnessName, nonce string) (runLayout, error) {
	absolute, err := filepath.Abs(runRoot)
	if err != nil {
		return runLayout{}, err
	}
	root := filepath.Join(absolute, harnessName+"-"+nonce)
	layout := runLayout{
		root:         root,
		workdir:      filepath.Join(root, "workdir"),
		home:         filepath.Join(root, "home"),
		claudeConfig: filepath.Join(root, "claude-config"),
		codexHome:    filepath.Join(root, "codex-home"),
		xdgConfig:    filepath.Join(root, "xdg", "config"),
		xdgData:      filepath.Join(root, "xdg", "data"),
		xdgCache:     filepath.Join(root, "xdg", "cache"),
	}
	for _, directory := range []string{layout.workdir, layout.home, layout.claudeConfig, layout.codexHome, layout.xdgConfig, layout.xdgData, layout.xdgCache} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return runLayout{}, err
		}
	}
	return layout, nil
}

// baseEnvironment points every harness's home and XDG state into the run.
func baseEnvironment(layout runLayout) []string {
	return []string{
		"HOME=" + layout.home,
		"XDG_CONFIG_HOME=" + layout.xdgConfig,
		"XDG_DATA_HOME=" + layout.xdgData,
		"XDG_CACHE_HOME=" + layout.xdgCache,
	}
}

// runHarness drives one harness through the three phases against the running
// stack, then checks the marker files and the plugin's phase events.
func runHarness(driver harnessDriver) error {
	base := shared.AdminBaseURL()
	if err := shared.WaitReady(base); err != nil {
		return err
	}
	if err := ensureBootstrapped(base); err != nil {
		return err
	}
	target, err := discoverTarget(base, driver.bareModel())
	if err != nil {
		return err
	}
	nonce, err := newRunNonce()
	if err != nil {
		return err
	}
	layout, err := newRunLayout(shared.GetenvPath("HARNESS_RUN_ROOT", "~/.cache/llm-router/harness"), driver.name(), nonce)
	if err != nil {
		return err
	}
	timeout, err := phaseTimeout()
	if err != nil {
		return err
	}
	extra, err := driver.prepare(layout, target)
	if err != nil {
		return err
	}
	environment := append(baseEnvironment(layout), extra...)

	sessionID := driver.newSessionID()
	shared.Stepf("%s: model %s, run %s, artifacts %s", driver.name(), target.routerModel, nonce, layout.root)
	for phase := 1; phase <= phaseCount; phase++ {
		turn := turnRequest{prompt: phasePrompt(phase, nonce), sessionID: sessionID, routerModel: target.routerModel}
		arguments := driver.resumeArguments(turn)
		if phase == 1 {
			arguments = driver.initialArguments(turn)
		}
		stdout, err := runInvocation(layout, driver.binary(), arguments, environment, timeout, phase)
		if err != nil {
			return err
		}
		outcome, err := driver.parseTurn(stdout, sessionID)
		if err != nil {
			return fmt.Errorf("phase %d: %w", phase, err)
		}
		if sessionID == "" {
			sessionID = outcome.sessionID
		}
		if outcome.sessionID != sessionID {
			return fmt.Errorf("phase %d continued session %q, want %q", phase, outcome.sessionID, sessionID)
		}
		if !strings.Contains(outcome.finalText, phaseReplies[phase]) {
			return fmt.Errorf("phase %d reply %q lacks %q", phase, outcome.finalText, phaseReplies[phase])
		}
		shared.OKf("%s phase %d: %s", driver.name(), phase, phaseReplies[phase])
	}

	if err := checkMarkerFiles(layout.workdir); err != nil {
		return err
	}
	entries, err := fetchRunEvents(base, nonce)
	if err != nil {
		return err
	}
	summary, err := checkEventSequence(entries)
	if err != nil {
		return err
	}
	shared.OKf("%s: marker files verified, %s", driver.name(), summary)
	return nil
}

// runInvocation runs one harness command in the run workdir. Stdin is left
// unset so the command reads /dev/null, and stdout and stderr are kept as
// artifacts for every phase.
func runInvocation(layout runLayout, binary string, arguments, environment []string, timeout time.Duration, phase int) ([]byte, error) {
	executable, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("phase %d: %s is not on PATH: %w", phase, binary, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = layout.workdir
	command.Env = append(os.Environ(), environment...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if err := os.WriteFile(filepath.Join(layout.root, fmt.Sprintf("phase-%d.stdout", phase)), stdout.Bytes(), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(layout.root, fmt.Sprintf("phase-%d.stderr", phase)), stderr.Bytes(), 0o644); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return stdout.Bytes(), fmt.Errorf("phase %d: %s timed out after %s", phase, binary, timeout)
	}
	if runErr != nil {
		return stdout.Bytes(), fmt.Errorf("phase %d: %s exited: %w: %s", phase, binary, runErr, tailText(stderr.String(), 1200))
	}
	return stdout.Bytes(), nil
}

// checkMarkerFiles verifies the files the shell tool calls wrote, with exact
// contents. A harness that never ran the tool leaves them missing.
func checkMarkerFiles(workdir string) error {
	for name, want := range markerFiles {
		raw, err := os.ReadFile(filepath.Join(workdir, name))
		if err != nil {
			return fmt.Errorf("marker file %s: %w", name, err)
		}
		if string(raw) != want {
			return fmt.Errorf("marker file %s holds %q, want %q", name, string(raw), want)
		}
	}
	return nil
}

// phaseTimeout reads HARNESS_PHASE_TIMEOUT, which bounds each invocation.
func phaseTimeout() (time.Duration, error) {
	raw := shared.Getenv("HARNESS_PHASE_TIMEOUT", "5m")
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("invalid HARNESS_PHASE_TIMEOUT %q: want a positive duration like 5m", raw)
	}
	return timeout, nil
}

// tailText returns the last limit bytes of text for error messages.
func tailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return "..." + text[len(text)-limit:]
}
