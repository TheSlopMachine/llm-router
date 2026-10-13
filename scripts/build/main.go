package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	version := shared.ResolveVersion()
	platforms := shared.RequireEnv("PLATFORMS")

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	// Strict: frontend build must succeed.
	shared.Stepf("Building frontend (vite build)...")
	if err := shared.RunViteBuild(root); err != nil {
		shared.Failf("%v", err)
	}

	gitCommit := shared.GitCommit()
	buildTime := shared.BuildTime()

	fmt.Printf("\n== Build - %s ==\n", version)
	fmt.Printf("  Commit:     %s\n", gitCommit)
	fmt.Printf("  Build time: %s\n\n", buildTime)

	publishDir := filepath.Join(root, "build", "release")
	_ = os.RemoveAll(publishDir)
	if err := os.MkdirAll(publishDir, 0755); err != nil {
		shared.Failf("create %s: %v", publishDir, err)
	}

	ldflags := shared.ReleaseLdflags(version, gitCommit, buildTime)

	platformList := strings.Fields(platforms)
	shared.Stepf("Building %d platforms...", len(platformList))

	var checksums []string
	for _, platform := range platformList {
		parts := strings.SplitN(platform, "/", 2)
		if len(parts) != 2 {
			shared.Failf("invalid platform %q: must be GOOS/GOARCH", platform)
		}
		goos, goarch := parts[0], parts[1]
		binaryName := shared.BinaryName(goos)
		outputDirectory := filepath.Join(publishDir, goos+"_"+goarch)
		if err := os.MkdirAll(outputDirectory, 0755); err != nil {
			shared.Failf("create %s: %v", outputDirectory, err)
		}
		outputBinary := filepath.Join(outputDirectory, binaryName)
		shared.Stepf("Building %s/%s...", goos, goarch)
		if err := shared.BuildBinary(root, goos, goarch, outputBinary, ldflags); err != nil {
			shared.Failf("%v", err)
		}

		zipName := fmt.Sprintf("%s_%s_%s.zip", shared.BinaryFileName, goos, goarch)
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
