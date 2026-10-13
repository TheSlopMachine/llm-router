// install builds the release binary for the host platform and places it
// into ~/.local/bin, replacing any previous install. Human-only: it writes
// outside the repository and may register the directory on PATH.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	binDirectory, err := shared.InstallBinDirectory()
	if err != nil {
		shared.Failf("%v", err)
	}
	destination := filepath.Join(binDirectory, shared.BinaryName(targetOS))

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	version := shared.ResolveVersion()
	gitCommit := shared.GitCommit()
	buildTime := shared.BuildTime()

	fmt.Printf("\n== Install - %s ==\n", version)
	fmt.Printf("  Commit:     %s\n", gitCommit)
	fmt.Printf("  Build time: %s\n", buildTime)
	fmt.Printf("  Target:     %s/%s\n", targetOS, targetArch)
	fmt.Printf("  Destination: %s\n\n", destination)

	refuseTrackedRunning(destination)
	refuseLockedDestination(destination)

	// Strict: frontend build must succeed.
	shared.Stepf("Building frontend (vite build)...")
	if err := shared.RunViteBuild(root); err != nil {
		shared.Failf("%v", err)
	}

	if err := os.MkdirAll(binDirectory, 0755); err != nil {
		shared.Failf("create %s: %v", binDirectory, err)
	}

	ldflags := shared.ReleaseLdflags(version, gitCommit, buildTime)
	staging := filepath.Join(binDirectory, shared.BinaryName(targetOS)+".install-tmp")
	shared.Stepf("Building %s/%s...", targetOS, targetArch)
	if err := shared.BuildBinary(root, targetOS, targetArch, staging, ldflags); err != nil {
		_ = os.Remove(staging)
		shared.Failf("%v", err)
	}
	if targetOS != "windows" {
		if err := os.Chmod(staging, 0755); err != nil {
			_ = os.Remove(staging)
			shared.Failf("chmod staging binary: %v", err)
		}
	}

	if _, err := os.Stat(destination); err == nil {
		if err := os.Remove(destination); err != nil {
			_ = os.Remove(staging)
			shared.Failf("remove previous install %s (stop the running app first): %v", destination, err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.Remove(staging)
		shared.Failf("inspect install destination %s: %v", destination, err)
	}
	if err := os.Rename(staging, destination); err != nil {
		_ = os.Remove(staging)
		shared.Failf("place install %s (stop the running app first): %v", destination, err)
	}
	shared.OKf("Installed %s", destination)

	ensurePath(binDirectory)
}

// refuseTrackedRunning stops the install when the dev pidfile tracks a
// live backend running the install destination itself.
func refuseTrackedRunning(destination string) {
	pidFile, err := shared.ExpandPath(shared.DefaultPidFile())
	if err != nil {
		return
	}
	process, err := shared.ReadPidFile(pidFile)
	if err != nil {
		return
	}
	if process.Backend <= 0 || strings.TrimSpace(process.BackendPath) == "" {
		return
	}
	if shared.AliveMatches(process.Backend, process.BackendPath) &&
		shared.SameExecutable(process.BackendPath, destination) {
		shared.Failf("running install found as PID %d; stop it first", process.Backend)
	}
}

// refuseLockedDestination stops the install when the destination exists
// but is not writable. A running executable denies write access (ETXTBSY
// on Linux, sharing violation on Windows), so this doubles as the running
// check on those platforms.
func refuseLockedDestination(destination string) {
	if _, err := os.Stat(destination); err != nil {
		return
	}
	probe, err := os.OpenFile(destination, os.O_WRONLY, 0)
	if err != nil {
		shared.Failf("install destination %s is not writable (stop the running app first): %v", destination, err)
		return
	}
	_ = probe.Close()
}

// ensurePath reports PATH state and registers the directory where safe.
// Unix shells own their rc files, so installs there only warn with setup
// steps and never edit files. Windows keeps one per-user PATH value, so
// the install appends there once when missing.
func ensurePath(binDirectory string) {
	if shared.BinDirectoryInPath(binDirectory) {
		shared.OKf("Install directory is on PATH")
		return
	}
	if runtime.GOOS == "windows" {
		if err := shared.RegisterUserPathWindows(binDirectory); err != nil {
			fmt.Printf("[WARN] user PATH registration failed: %v\n", err)
			fmt.Print(shared.PathSetupHint(binDirectory))
			return
		}
		shared.OKf("Registered install directory on user PATH (open a new shell)")
		return
	}
	fmt.Printf("[WARN] install directory is not on PATH\n")
	fmt.Print(shared.PathSetupHint(binDirectory))
}
