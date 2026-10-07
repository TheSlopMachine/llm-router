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

func parseTriple(s string) (triple, error) {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "v")
	t = strings.TrimPrefix(t, "V")
	parts := strings.Split(t, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return triple{}, fmt.Errorf("expected X.Y[.Z], got %q", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return triple{}, fmt.Errorf("invalid empty component in %q", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return triple{}, fmt.Errorf("invalid numeric component %q in %q", p, s)
		}
		nums[i] = n
	}
	return triple{major: nums[0], minor: nums[1], fix: nums[2]}, nil
}

func (t triple) tag() string {
	if t.fix == 0 {
		return fmt.Sprintf("v%d.%d", t.major, t.minor)
	}
	return fmt.Sprintf("v%d.%d.%d", t.major, t.minor, t.fix)
}

func (t triple) String() string {
	return strings.TrimPrefix(t.tag(), "v")
}

func (t triple) less(o triple) bool {
	if t.major != o.major {
		return t.major < o.major
	}
	if t.minor != o.minor {
		return t.minor < o.minor
	}
	return t.fix < o.fix
}

func git(args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		shared.Failf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func gitOK(args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func main() {
	typ := strings.ToLower(strings.TrimSpace(os.Getenv("TYPE")))
	forced := strings.TrimSpace(os.Getenv("VERSION"))
	if forced == "dev" {
		forced = ""
	}
	notes := os.Getenv("NOTES")
	if typ != "" && forced != "" {
		shared.Failf("TYPE and VERSION are mutually exclusive: set one")
	}
	if typ == "" && forced == "" {
		shared.Failf("set TYPE=fix|minor|major or VERSION=X.Y[.Z]")
	}

	// Preflight: git checkout, clean tree, synced with origin/main.
	if _, ok := gitOK("rev-parse", "--git-dir"); !ok {
		shared.Failf("not a git checkout: publish requires git tags")
	}
	if status := git("status", "--porcelain"); status != "" {
		shared.Failf("tree is dirty: commit or stash first:\n%s", status)
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
	case typ == "fix":
		target = triple{major: latest.major, minor: latest.minor, fix: latest.fix + 1}
	case typ == "minor":
		target = triple{major: latest.major, minor: latest.minor + 1}
	case typ == "major":
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
	args := []string{"tag", "-a", tagName, "-m", "Release " + tagName}
	if strings.TrimSpace(notes) != "" {
		args = append(args, "-m", notes)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		shared.Failf("git tag %s: %v\n%s", tagName, err, string(out))
	}
	if out, err := exec.Command("git", "push", "origin", tagName).CombinedOutput(); err != nil {
		shared.Failf("git push origin %s: %v\n%s", tagName, err, string(out))
	}
	fmt.Printf("\n[OK] Published %s (from %s, commit %s)\n", tagName, latestRaw, head)
	fmt.Printf("[OK] CI builds on tag push\n")
}
