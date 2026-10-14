package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// phaseCount is the number of invocations per harness run. Phase 1 starts the
// session and phases 2 and 3 resume it, so history must carry across them.
const phaseCount = 3

// phaseTasks describe each phase for the run log. The mock plugin decides the
// shell commands itself, so these texts are for humans reading the output.
var phaseTasks = map[int]string{
	1: "create phase1.txt with the shell tool",
	2: "read phase1.txt and append phase2.txt",
	3: "read both marker files",
}

// phaseReplies are the final tokens the mock plugin returns per phase. The
// plugin owns them; TestPhaseReplyTokensMatchPlugin keeps both sides in step.
var phaseReplies = map[int]string{
	1: "PHASE 1 COMPLETE",
	2: "PHASE 2 COMPLETE",
	3: "HARNESS-MOCK-OK",
}

// markerFiles are the files the phase tool calls write, with exact contents.
var markerFiles = map[string]string{
	"phase1.txt": "phase-1 ok\n",
	"phase2.txt": "phase-2 ok\n",
}

// routerTarget is the router model every harness run talks to.
type routerTarget struct {
	adminURL    string
	providerID  string
	bareModel   string
	routerModel string
}

// turnRequest describes one harness invocation.
type turnRequest struct {
	prompt      string
	sessionID   string
	routerModel string
	// workdir is the run work directory the harness must operate in.
	// Drivers pass it through (opencode --dir) instead of relying on the
	// child process working directory, which nested servers may not honor.
	workdir string
}

// turnOutcome is what one invocation reports back.
type turnOutcome struct {
	sessionID string
	finalText string
}

// harnessDriver adapts one CLI harness to the shared three-phase run. Each
// method maps onto a documented flag or environment variable of that CLI.
type harnessDriver interface {
	// name is the HARNESS value and the run directory prefix.
	name() string
	// binary is the executable looked up on PATH.
	binary() string
	// bareModel is the model name the mock plugin declares for this harness.
	bareModel() string
	// newSessionID returns an identifier the driver assigns, or "" when the
	// harness assigns its own on the first turn.
	newSessionID() string
	// prepare writes the harness configuration and returns extra environment.
	prepare(layout runLayout, target routerTarget) ([]string, error)
	// initialArguments builds the argument vector of phase 1.
	initialArguments(turn turnRequest) []string
	// resumeArguments builds the argument vector of phases 2 and 3.
	resumeArguments(turn turnRequest) []string
	// parseTurn reads one invocation's stdout. expectedSessionID is "" on the
	// first turn for harnesses that assign their own identifier.
	parseTurn(stdout []byte, expectedSessionID string) (turnOutcome, error)
}

// driverNamed returns the driver for a HARNESS value.
func driverNamed(name string) (harnessDriver, error) {
	switch name {
	case "claude":
		return claudeDriver{}, nil
	case "codex":
		return codexDriver{}, nil
	case "opencode":
		return opencodeDriver{}, nil
	}
	return nil, fmt.Errorf("unknown HARNESS %q: want claude, codex or opencode", name)
}

// phasePrompt is the user turn of one phase. The marker lets the plugin find
// the phase, and run= lets the driver isolate this run's events.
func phasePrompt(phase int, nonce string) string {
	return fmt.Sprintf("PHASE %d run=%s: %s", phase, nonce, phaseTasks[phase])
}

// newRunNonce returns a random identifier that tags one run.
func newRunNonce() (string, error) {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

// newUUID returns a random RFC 4122 version 4 identifier.
func newUUID() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		panic(err)
	}
	random[6] = (random[6] & 0x0f) | 0x40
	random[8] = (random[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(random[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
