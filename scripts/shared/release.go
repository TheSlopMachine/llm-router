// Package shared release helpers: version stamping, frontend build and
// host binary construction shared by the build and install scripts. One
// owner for the release pipeline; build adds multi-platform zips while
// install adds single-host placement into ~/.local/bin.
package shared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bitfield/script"
)

// BinaryFileName is the release binary name without extension.
const BinaryFileName = "llm-router"

// InstallBinSuffix is the home-relative install location on every OS.
var InstallBinSuffix = filepath.Join(".local", "bin")

// BinaryName returns the binary file name for targetOS (.exe on Windows).
func BinaryName(targetOS string) string {
	if targetOS == "windows" {
		return BinaryFileName + ".exe"
	}
	return BinaryFileName
}

// ResolveVersion prefers explicit VERSION, then the latest git tag, then
// dev. The release stays fail-open without git.
func ResolveVersion() string {
	if versionValue := strings.TrimSpace(os.Getenv("VERSION")); versionValue != "" && versionValue != "dev" {
		return versionValue
	}
	tag, err := script.Exec(`git describe --tags --abbrev=0 --match "v*"`).String()
	if err != nil {
		fmt.Printf("[WARN] no git tag found, using dev version\n")
		return "dev"
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		fmt.Printf("[WARN] empty git tag, using dev version\n")
		return "dev"
	}
	return tag
}

// GitCommit returns the short HEAD hash or unknown when git is unavailable.
func GitCommit() string {
	commit, err := script.Exec("git rev-parse --short HEAD").String()
	if err != nil {
		fmt.Printf("[WARN] git rev-parse --short HEAD failed, using unknown commit: %v\n", err)
		return "unknown"
	}
	commit = strings.TrimSpace(commit)
	if commit == "" {
		fmt.Printf("[WARN] git rev-parse returned empty commit, using unknown\n")
		return "unknown"
	}
	return commit
}

// BuildTime returns the current UTC time in RFC3339 for version stamping.
func BuildTime() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ReleaseLdflags renders the linker flags stamping version metadata.
func ReleaseLdflags(version, commit, buildTime string) string {
	return fmt.Sprintf("-s -w -X main.Version=%s -X main.GitCommit=%s -X main.BuildTime=%s",
		version, commit, buildTime)
}

// RunViteBuild compiles the frontend into the embedded dashboard build.
// Strict: any vite failure is fatal to the caller.
func RunViteBuild(root string) error {
	Stepf("Running vite build...")
	webDirectory := filepath.Join(root, "web")
	cmd := exec.Command("bun", "run", "build")
	cmd.Dir = webDirectory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("vite build: %w", err)
	}
	return nil
}

// BuildBinary compiles one GOOS/GOARCH binary with the given linker flags.
// Strict: any go failure is fatal to the caller.
func BuildBinary(root, targetOS, targetArch, output, ldflags string) error {
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", output, ".")
	cmd.Dir = root
	cmd.Env = EnvWith([]string{"GOOS=" + targetOS, "GOARCH=" + targetArch})
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s/%s: %w", targetOS, targetArch, err)
	}
	return nil
}

// InstallBinDirectory returns the fixed install location (home plus
// .local/bin on every OS). It fails when the home directory is unknown.
func InstallBinDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("resolve home directory for install: %w", ErrHomeDirectoryUnresolvable)
	}
	return filepath.Join(home, InstallBinSuffix), nil
}

// BinDirectoryInPath reports whether directory is already on PATH.
// Comparison cleans separators and ignores case on Windows.
func BinDirectoryInPath(directory string) bool {
	wanted := filepath.Clean(strings.TrimSpace(directory))
	if wanted == "" {
		return false
	}
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		cleaned := filepath.Clean(strings.TrimSpace(entry))
		if cleaned == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			if strings.EqualFold(cleaned, wanted) {
				return true
			}
			continue
		}
		if cleaned == wanted {
			return true
		}
	}
	return false
}

// PathSetupHint prints per-OS steps adding directory to PATH. Unix shells
// differ per file, so the hint names the file instead of editing it.
func PathSetupHint(directory string) string {
	var builder strings.Builder
	builder.WriteString("Add the install directory to PATH:\n")
	switch runtime.GOOS {
	case "windows":
		fmt.Fprintf(&builder, "  [PowerShell] [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User') + ';%s', 'User')\n", directory)
		builder.WriteString("  Then open a new shell.\n")
	default:
		if strings.HasSuffix(strings.TrimSpace(os.Getenv("SHELL")), "fish") {
			fmt.Fprintf(&builder, "  fish_add_path %s\n", directory)
		} else {
			fmt.Fprintf(&builder, "  export PATH=\"%s:$PATH\"\n", directory)
			builder.WriteString("  Persist it in ~/.profile (bash login, sh, dash) or ~/.zprofile (zsh login).\n")
		}
		builder.WriteString("  Then open a new shell.\n")
	}
	return builder.String()
}

// RegisterUserPathWindows appends directory to the per-user PATH once when
// missing. System PATH stays untouched. Failures return an error for the
// caller to degrade into printed setup steps.
func RegisterUserPathWindows(directory string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("windows PATH registration on %s", runtime.GOOS)
	}
	current, err := powershellUserPath()
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(current, ";") {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(entry)), filepath.Clean(directory)) {
			return nil
		}
	}
	joined := strings.TrimSpace(current)
	if joined == "" {
		joined = directory
	} else {
		joined += ";" + directory
	}
	escaped := strings.ReplaceAll(joined, "'", "''")
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"[Environment]::SetEnvironmentVariable('Path', '"+escaped+"', 'User')")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set user PATH: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// powershellUserPath reads the per-user PATH through PowerShell without
// touching the registry package, so this file compiles on every platform.
func powershellUserPath() (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"[Environment]::GetEnvironmentVariable('Path','User')")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read user PATH: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
