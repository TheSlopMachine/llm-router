package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"

	"github.com/bitfield/script"
)

const binary = "llm-router"

func main() {
	version := resolveVersion()
	platforms := shared.RequireEnv("PLATFORMS")

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	// Strict: frontend build must succeed.
	shared.Stepf("Building frontend (vite build)...")
	if err := runViteBuild(root); err != nil {
		shared.Failf("%v", err)
	}

	gitCommit, err := script.Exec("git rev-parse --short HEAD").String()
	if err != nil {
		fmt.Printf("[WARN] git rev-parse --short HEAD failed, using unknown commit: %v\n", err)
		gitCommit = "unknown"
	}
	gitCommit = strings.TrimSpace(gitCommit)
	if gitCommit == "" {
		fmt.Printf("[WARN] git rev-parse returned empty commit, using unknown\n")
		gitCommit = "unknown"
	}
	buildTime := time.Now().UTC().Format(time.RFC3339)

	fmt.Printf("\n== Build - %s ==\n", version)
	fmt.Printf("  Commit:     %s\n", gitCommit)
	fmt.Printf("  Build time: %s\n\n", buildTime)

	publishDir := filepath.Join(root, "build", "release")
	_ = os.RemoveAll(publishDir)
	if err := os.MkdirAll(publishDir, 0755); err != nil {
		shared.Failf("create %s: %v", publishDir, err)
	}

	ldflags := fmt.Sprintf("-s -w -X main.Version=%s -X main.GitCommit=%s -X main.BuildTime=%s",
		version, gitCommit, buildTime)

	platformList := strings.Fields(platforms)
	shared.Stepf("Building %d platforms...", len(platformList))

	var checksums []string
	for _, platform := range platformList {
		parts := strings.SplitN(platform, "/", 2)
		if len(parts) != 2 {
			shared.Failf("invalid platform %q: must be GOOS/GOARCH", platform)
		}
		goos, goarch := parts[0], parts[1]
		binaryName := binary
		if goos == "windows" {
			binaryName += ".exe"
		}
		outputDirectory := filepath.Join(publishDir, goos+"_"+goarch)
		if err := os.MkdirAll(outputDirectory, 0755); err != nil {
			shared.Failf("create %s: %v", outputDirectory, err)
		}
		outputBinary := filepath.Join(outputDirectory, binaryName)
		shared.Stepf("Building %s/%s...", goos, goarch)
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", outputBinary, ".")
		cmd.Dir = root
		cmd.Env = shared.EnvWith([]string{"GOOS=" + goos, "GOARCH=" + goarch})
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			shared.Failf("go build %s/%s: %v", goos, goarch, err)
		}

		zipName := fmt.Sprintf("%s_%s_%s.zip", binary, goos, goarch)
		zipPath := filepath.Join(outputDirectory, zipName)
		if err := zipFile(zipPath, outputBinary); err != nil {
			shared.Failf("zip %s/%s: %v", goos, goarch, err)
		}

		checksum, err := sha256File(zipPath)
		if err != nil {
			shared.Failf("sha256 %s: %v", zipPath, err)
		}
		line := fmt.Sprintf("%s  %s_%s/%s", checksum, goos, goarch, zipName)
		checksums = append(checksums, line)
		shared.OKf("Done: %s/%s", goos, goarch)
	}

	sort.Strings(checksums)
	checksumPath := filepath.Join(publishDir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(strings.Join(checksums, "\n")+"\n"), 0644); err != nil {
		shared.Failf("write checksums.txt: %v", err)
	}

	fmt.Printf("\n[OK] Artifacts in  %s/\n", publishDir)
	fmt.Printf("[OK] Checksums in  %s\n", checksumPath)
}

// resolveVersion prefers explicit VERSION, then the latest git tag,
// then dev. Build stays fail-open without git.
func resolveVersion() string {
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

func runViteBuild(root string) error {
	shared.Stepf("Running vite build...")
	webDir := filepath.Join(root, "web")
	cmd := exec.Command("bun", "run", "build")
	cmd.Dir = webDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("vite build: %w", err)
	}
	return nil
}

func zipFile(dst, src string) error {
	outputFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer outputFile.Close()
	zipWriter := zip.NewWriter(outputFile)
	defer zipWriter.Close()

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.Base(src)
	header.Method = zip.Deflate
	fileWriter, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	inputFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer inputFile.Close()
	_, err = io.Copy(fileWriter, inputFile)
	return err
}

func sha256File(path string) (string, error) {
	inputFile, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer inputFile.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, inputFile); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
