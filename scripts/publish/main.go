// Command publish creates and pushes the next release tag.
// Tags are the source of truth: vX.Y.Z, short vX.Y when Fix is 0.
// Version math duplicates internal/version (separate Go module by design).
//
// Usage: make publish TYPE=fix|minor|major [NOTES=...]
//
//	or: make publish VERSION=X.Y[.Z] [NOTES=...] (force a version)
//
// TYPE and VERSION are mutually exclusive. The tree must be clean and
// HEAD must equal origin/main. This command never builds: CI builds on tag.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

type triple struct {
	major, minor, fix int
}

func parseTriple(tag string) (triple, error) {
	trimmed := strings.TrimSpace(tag)
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")
	parts := strings.Split(trimmed, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return triple{}, fmt.Errorf("expected X.Y[.Z], got %q", tag)
	}
	numbers := make([]int, 3)
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return triple{}, fmt.Errorf("invalid empty component in %q", tag)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return triple{}, fmt.Errorf("invalid numeric component %q in %q", part, tag)
		}
		numbers[i] = number
	}
	return triple{major: numbers[0], minor: numbers[1], fix: numbers[2]}, nil
}

func (receiver triple) tag() string {
	if receiver.fix == 0 {
		return fmt.Sprintf("v%d.%d", receiver.major, receiver.minor)
	}
	return fmt.Sprintf("v%d.%d.%d", receiver.major, receiver.minor, receiver.fix)
}

func (receiver triple) String() string {
	return strings.TrimPrefix(receiver.tag(), "v")
}

func (receiver triple) less(other triple) bool {
	if receiver.major != other.major {
		return receiver.major < other.major
	}
	if receiver.minor != other.minor {
		return receiver.minor < other.minor
	}
	return receiver.fix < other.fix
}

func git(args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		shared.Failf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func gitOK(args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = nil
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func main() {
	releaseType := strings.ToLower(strings.TrimSpace(os.Getenv("TYPE")))
	forced := strings.TrimSpace(os.Getenv("VERSION"))
	if forced == "dev" {
		forced = ""
	}
	notes := os.Getenv("NOTES")
	if releaseType != "" && forced != "" {
		shared.Failf("TYPE and VERSION are mutually exclusive: set one")
	}
	if releaseType == "" && forced == "" {
		shared.Failf("set TYPE=fix|minor|major or VERSION=X.Y[.Z]")
	}

	// Preflight: git checkout, clean tree, synced with origin/main.
	if _, ok := gitOK("rev-parse", "--git-dir"); !ok {
		shared.Failf("not a git checkout: publish requires git tags")
	}
	if treeStatus := git("status", "--porcelain"); treeStatus != "" {
		shared.Failf("tree is dirty: commit or stash first:\n%s", treeStatus)
	}
	branch := git("rev-parse", "--abbrev-ref", "HEAD")
	if branch != "main" {
		shared.Failf("publish only from main, current branch is %q", branch)
	}
	if _, ok := gitOK("fetch", "origin"); !ok {
		shared.Failf("git fetch origin failed")
	}
	head := git("rev-parse", "HEAD")
	upstream, ok := gitOK("rev-parse", "origin/main")
	if !ok {
		shared.Failf("origin/main is unreachable: push main first")
	}
	if head != upstream {
		shared.Failf("HEAD (%s) differs from origin/main (%s): push or pull first", head, upstream)
	}

	latestRaw, ok := gitOK("describe", "--tags", "--abbrev=0", "--match", "v*")
	if !ok || latestRaw == "" {
		if forced == "" {
			shared.Failf("no tags found: bootstrap with make publish VERSION=X.Y.Z")
		}
		latestRaw = ""
	}
	var latest triple
	if latestRaw != "" {
		var err error
		latest, err = parseTriple(latestRaw)
		if err != nil {
			shared.Failf("latest tag %q is not a version: %v", latestRaw, err)
		}
		if tagCommit, ok := gitOK("rev-list", "-n", "1", latestRaw); ok && tagCommit == head {
			shared.Failf("nothing changed since %s: HEAD already tagged", latestRaw)
		}
	}

	var target triple
	switch {
	case forced != "":
		var err error
		target, err = parseTriple(forced)
		if err != nil {
			shared.Failf("invalid VERSION %q: %v", forced, err)
		}
		if _, exists := gitOK("rev-parse", "-q", "--verify", "refs/tags/"+target.tag()); exists {
			shared.Failf("tag %s already exists", target.tag())
		}
		if latestRaw != "" && target.less(latest) {
			fmt.Printf("[WARN] target %s is older than latest %s\n", target.tag(), latestRaw)
		}
	case releaseType == "fix":
		target = triple{major: latest.major, minor: latest.minor, fix: latest.fix + 1}
	case releaseType == "minor":
		target = triple{major: latest.major, minor: latest.minor + 1}
	case releaseType == "major":
		target = triple{major: latest.major + 1}
	default:
		shared.Failf("invalid TYPE %q: want fix|minor|major", os.Getenv("TYPE"))
	}
	if latestRaw != "" {
		if _, exists := gitOK("rev-parse", "-q", "--verify", "refs/tags/"+target.tag()); exists {
			shared.Failf("tag %s already exists", target.tag())
		}
	}

	tagName := target.tag()
	arguments := []string{"tag", "-a", tagName, "-m", "Release " + tagName}
	if strings.TrimSpace(notes) != "" {
		arguments = append(arguments, "-m", notes)
	}
	if output, err := exec.Command("git", arguments...).CombinedOutput(); err != nil {
		shared.Failf("git tag %s: %v\n%s", tagName, err, string(output))
	}
	if output, err := exec.Command("git", "push", "origin", tagName).CombinedOutput(); err != nil {
		shared.Failf("git push origin %s: %v\n%s", tagName, err, string(output))
	}
	fmt.Printf("\n[OK] Published %s (from %s, commit %s)\n", tagName, latestRaw, head)
	fmt.Printf("[OK] CI builds on tag push\n")
}
