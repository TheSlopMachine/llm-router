package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// platform is one GOOS/GOARCH build target.
type platform struct {
	targetOS   string
	targetArch string
}

func (target platform) String() string {
	return target.targetOS + "/" + target.targetArch
}

func main() {
	version := shared.ResolveVersion()
	platforms, err := parsePlatforms(shared.RequireEnv("PLATFORMS"))
	if err != nil {
		shared.Failf("%v", err)
	}
	workers, err := buildWorkers()
	if err != nil {
		shared.Failf("%v", err)
	}

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	// Strict: frontend build must succeed.
	shared.Stepf("Building frontend (vite build)...")
	if err := shared.RunViteBuild(root); err != nil {
		shared.Failf("%v", err)
	}

	// Strict: the router embeds the Docker gateway built for linux.
	if err := shared.EnsureGatewayPayload(root, true); err != nil {
		shared.Failf("%v", err)
	}

	gitCommit := shared.GitCommit()
	buildTime := shared.BuildTime()

	fmt.Printf("\n== Build - %s ==\n", version)
	fmt.Printf("  Commit:     %s\n", gitCommit)
	fmt.Printf("  Build time: %s\n\n", buildTime)

	publishDirectory := filepath.Join(root, "build", "release")
	_ = os.RemoveAll(publishDirectory)
	if err := os.MkdirAll(publishDirectory, 0755); err != nil {
		shared.Failf("create %s: %v", publishDirectory, err)
	}

	ldflags := shared.ReleaseLdflags(version, gitCommit, buildTime)

	shared.Stepf("Building %d platforms with %d workers...", len(platforms), workers)
	checksums, err := buildPlatforms(root, publishDirectory, ldflags, platforms, workers)
	if err != nil {
		shared.Failf("%v", err)
	}

	sort.Strings(checksums)
	checksumPath := filepath.Join(publishDirectory, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(strings.Join(checksums, "\n")+"\n"), 0644); err != nil {
		shared.Failf("write checksums.txt: %v", err)
	}

	fmt.Printf("\n[OK] Artifacts in  %s/\n", publishDirectory)
	fmt.Printf("[OK] Checksums in  %s\n", checksumPath)
}

// parsePlatforms splits PLATFORMS into GOOS/GOARCH targets. Every entry is
// checked before any build starts.
func parsePlatforms(value string) ([]platform, error) {
	var platforms []platform
	for _, field := range strings.Fields(value) {
		targetOS, targetArch, found := strings.Cut(field, "/")
		if !found || targetOS == "" || targetArch == "" || strings.Contains(targetArch, "/") {
			return nil, fmt.Errorf("invalid platform %q: must be GOOS/GOARCH", field)
		}
		platforms = append(platforms, platform{targetOS: targetOS, targetArch: targetArch})
	}
	return platforms, nil
}

// buildWorkers reads BUILD_WORKERS. Blank means one worker per CPU.
func buildWorkers() (int, error) {
	value := strings.TrimSpace(os.Getenv("BUILD_WORKERS"))
	if value == "" {
		return runtime.NumCPU(), nil
	}
	workers, err := strconv.Atoi(value)
	if err != nil || workers < 1 {
		return 0, fmt.Errorf("invalid BUILD_WORKERS %q: must be a positive integer", value)
	}
	return workers, nil
}

// buildPlatforms builds, zips and hashes every platform, at most workers at
// once. It returns one checksums.txt line per platform. The first failure
// stops the running builds and becomes the returned error.
func buildPlatforms(root, publishDirectory, ldflags string, platforms []platform, workers int) ([]string, error) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	queue := make(chan platform)
	// outputMutex keeps each platform's output in one block and guards checksums.
	var outputMutex sync.Mutex
	var checksums []string
	var group sync.WaitGroup
	for range min(workers, len(platforms)) {
		group.Go(func() {
			for target := range queue {
				outputMutex.Lock()
				shared.Stepf("Building %s...", target)
				outputMutex.Unlock()

				var log bytes.Buffer
				line, err := buildPlatform(ctx, root, publishDirectory, ldflags, target, &log)

				outputMutex.Lock()
				switch {
				case err == nil:
					os.Stdout.Write(log.Bytes())
					checksums = append(checksums, line)
					shared.OKf("Done: %s", target)
				case ctx.Err() == nil:
					os.Stdout.Write(log.Bytes())
					cancel(err)
				}
				outputMutex.Unlock()
			}
		})
	}

feed:
	for _, target := range platforms {
		select {
		case queue <- target:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	group.Wait()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return checksums, nil
}

// buildPlatform compiles one platform into publishDirectory/GOOS_GOARCH,
// zips the binary and returns its checksums.txt line.
func buildPlatform(ctx context.Context, root, publishDirectory, ldflags string, target platform, log io.Writer) (string, error) {
	directoryName := target.targetOS + "_" + target.targetArch
	outputDirectory := filepath.Join(publishDirectory, directoryName)
	if err := os.MkdirAll(outputDirectory, 0755); err != nil {
		return "", fmt.Errorf("create %s: %w", outputDirectory, err)
	}
	outputBinary := filepath.Join(outputDirectory, shared.BinaryName(target.targetOS))
	if err := shared.BuildBinary(ctx, root, target.targetOS, target.targetArch, outputBinary, ldflags, log); err != nil {
		return "", err
	}

	zipName := fmt.Sprintf("%s_%s.zip", shared.BinaryFileName, directoryName)
	zipPath := filepath.Join(outputDirectory, zipName)
	if err := zipFile(zipPath, outputBinary); err != nil {
		return "", fmt.Errorf("zip %s: %w", target, err)
	}

	checksum, err := sha256File(zipPath)
	if err != nil {
		return "", fmt.Errorf("sha256 %s: %w", zipPath, err)
	}
	return fmt.Sprintf("%s  %s/%s", checksum, directoryName, zipName), nil
}

// zipFile writes source as the only entry of the zip at destination. Close
// errors are returned: a failed close leaves a truncated archive.
func zipFile(destination, source string) error {
	outputFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	if err := writeZipEntry(outputFile, source); err != nil {
		outputFile.Close()
		return err
	}
	return outputFile.Close()
}

func writeZipEntry(output io.Writer, source string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.Base(source)
	header.Method = zip.Deflate
	zipWriter := zip.NewWriter(output)
	fileWriter, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	inputFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer inputFile.Close()
	if _, err := io.Copy(fileWriter, inputFile); err != nil {
		return err
	}
	return zipWriter.Close()
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
