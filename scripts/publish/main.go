package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
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
	version := flag.String("version", "dev", "release version for ldflags")
	platforms := flag.String("platforms", "", "space-separated GOOS/GOARCH list")
	remote := flag.String("remote", "https", "clone protocol for workspace")
	flag.Parse()

	if *platforms == "" {
		shared.Failf("missing --platforms")
	}
	if *remote != "https" && *remote != "ssh" {
		shared.Failf("invalid --remote %q", *remote)
	}

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	// Strict: frontend build must succeed.
	shared.Stepf("Building frontend (vite build)...")
	if err := shared.RunScript("frontend", "--mode", "build"); err != nil {
		shared.Failf("%v", err)
	}

	shared.Stepf("Preparing workspace...")
	if err := shared.RunScript("workspace", "--remote", *remote); err != nil {
		shared.Failf("%v", err)
	}

	gitCommit, err := script.Exec("git rev-parse --short HEAD").String()
	if err != nil {
		shared.Failf("git rev-parse --short HEAD: %v", err)
	}
	gitCommit = strings.TrimSpace(gitCommit)
	if gitCommit == "" {
		shared.Failf("git rev-parse returned empty commit")
	}
	buildTime := time.Now().UTC().Format(time.RFC3339)

	fmt.Printf("\n== Publish - %s ==\n", *version)
	fmt.Printf("  Commit:     %s\n", gitCommit)
	fmt.Printf("  Build time: %s\n\n", buildTime)

	publishDir := filepath.Join(root, "build", "release")
	_ = os.RemoveAll(publishDir)
	if err := os.MkdirAll(publishDir, 0755); err != nil {
		shared.Failf("create %s: %v", publishDir, err)
	}

	ldflags := fmt.Sprintf("-s -w -X main.Version=%s -X main.GitCommit=%s -X main.BuildTime=%s",
		*version, gitCommit, buildTime)

	plats := strings.Fields(*platforms)
	shared.Stepf("Building %d platforms...", len(plats))

	var checksums []string
	for _, plat := range plats {
		parts := strings.SplitN(plat, "/", 2)
		if len(parts) != 2 {
			shared.Failf("invalid platform %q: must be GOOS/GOARCH", plat)
		}
		goos, goarch := parts[0], parts[1]
		binName := binary
		if goos == "windows" {
			binName += ".exe"
		}
		outDir := filepath.Join(publishDir, goos+"_"+goarch)
		if err := os.MkdirAll(outDir, 0755); err != nil {
			shared.Failf("create %s: %v", outDir, err)
		}
		outBin := filepath.Join(outDir, binName)
		shared.Stepf("Building %s/%s...", goos, goarch)
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", outBin, ".")
		cmd.Dir = root
		cmd.Env = shared.EnvWith([]string{"GOOS=" + goos, "GOARCH=" + goarch})
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			shared.Failf("go build %s/%s: %v", goos, goarch, err)
		}

		zipName := fmt.Sprintf("%s_%s_%s.zip", binary, goos, goarch)
		zipPath := filepath.Join(outDir, zipName)
		if err := zipFile(zipPath, outBin); err != nil {
			shared.Failf("zip %s/%s: %v", goos, goarch, err)
		}

		sum, err := sha256File(zipPath)
		if err != nil {
			shared.Failf("sha256 %s: %v", zipPath, err)
		}
		line := fmt.Sprintf("%s  %s_%s/%s", sum, goos, goarch, zipName)
		checksums = append(checksums, line)
		shared.OKf("Done: %s/%s", goos, goarch)
	}

	sort.Strings(checksums)
	csPath := filepath.Join(publishDir, "checksums.txt")
	if err := os.WriteFile(csPath, []byte(strings.Join(checksums, "\n")+"\n"), 0644); err != nil {
		shared.Failf("write checksums.txt: %v", err)
	}

	fmt.Printf("\n[OK] Artifacts in  %s/\n", publishDir)
	fmt.Printf("[OK] Checksums in  %s\n", csPath)
}

func zipFile(dst, src string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	defer w.Close()

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.Base(src)
	hdr.Method = zip.Deflate
	fw, err := w.CreateHeader(hdr)
	if err != nil {
		return err
	}
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(fw, r)
	return err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
