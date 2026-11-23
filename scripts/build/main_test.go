package main

import (
	"runtime"
	"slices"
	"testing"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func TestParsePlatforms(t *testing.T) {
	platforms, err := parsePlatforms(" linux/amd64  windows/arm64 ")
	if err != nil {
		t.Fatalf("parsePlatforms: %v", err)
	}
	expected := []platform{{targetOS: "linux", targetArch: "amd64"}, {targetOS: "windows", targetArch: "arm64"}}
	if !slices.Equal(platforms, expected) {
		t.Fatalf("platforms = %v, expected %v", platforms, expected)
	}
	for _, value := range []string{"linux", "linux/", "/amd64", "linux/amd64/v2", "linux/amd64 darwin"} {
		if _, err := parsePlatforms(value); err == nil {
			t.Errorf("parsePlatforms(%q) accepted an invalid platform", value)
		}
	}
}

func TestBuildWorkers(t *testing.T) {
	t.Setenv("BUILD_WORKERS", "")
	if workers, err := buildWorkers(); err != nil || workers != runtime.NumCPU() {
		t.Fatalf("blank BUILD_WORKERS = %d, %v; expected %d", workers, err, runtime.NumCPU())
	}
	t.Setenv("BUILD_WORKERS", "3")
	if workers, err := buildWorkers(); err != nil || workers != 3 {
		t.Fatalf("BUILD_WORKERS=3 gives %d, %v", workers, err)
	}
	for _, value := range []string{"0", "-1", "four"} {
		t.Setenv("BUILD_WORKERS", value)
		if _, err := buildWorkers(); err == nil {
			t.Errorf("BUILD_WORKERS=%q accepted", value)
		}
	}
}

// TestBuildPlatformsStopsOnFailure checks that failing builds return an
// error and that the worker pool ends instead of blocking on the queue.
func TestBuildPlatformsStopsOnFailure(t *testing.T) {
	root, err := shared.RootDir()
	if err != nil {
		t.Fatalf("RootDir: %v", err)
	}
	platforms := []platform{
		{targetOS: "unknownos", targetArch: "amd64"},
		{targetOS: "unknownos", targetArch: "arm64"},
		{targetOS: "unknownos", targetArch: "386"},
		{targetOS: "unknownos", targetArch: "arm"},
	}
	checksums, err := buildPlatforms(root, t.TempDir(), "", platforms, 2)
	if err == nil {
		t.Fatalf("buildPlatforms succeeded for unsupported platforms")
	}
	if checksums != nil {
		t.Fatalf("checksums = %v, expected none on failure", checksums)
	}
}
