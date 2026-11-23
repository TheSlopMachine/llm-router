package shared

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GatewayArchitectures are the linux targets of the embedded Docker gateway.
var GatewayArchitectures = []string{"amd64", "arm64"}

// gatewaySourceDirectories hold every package the gateway program builds
// from. A newer .go file there rebuilds the payload.
var gatewaySourceDirectories = []string{
	filepath.Join("internal", "dockergateway"),
	filepath.Join("internal", "netguard"),
}

// GatewayPayloadDirectory is where the router embeds the gateway from.
func GatewayPayloadDirectory(root string) string {
	return filepath.Join(root, "internal", "services", "dockerhost", "payload")
}

// EnsureGatewayPayload cross-compiles the Docker gateway program for each
// linux architecture and gzips it into the embed directory. Up-to-date
// payloads are kept unless noSkip is set.
func EnsureGatewayPayload(root string, noSkip bool) error {
	newestSource, err := newestGoFile(root, gatewaySourceDirectories)
	if err != nil {
		return err
	}
	directory := GatewayPayloadDirectory(root)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create %s: %w", directory, err)
	}
	for _, architecture := range GatewayArchitectures {
		output := filepath.Join(directory, "gateway-linux-"+architecture+".gz")
		if !noSkip {
			if info, err := os.Stat(output); err == nil && info.ModTime().After(newestSource) {
				continue
			}
		}
		Stepf("Building Docker gateway linux/%s...", architecture)
		if err := buildGateway(root, architecture, output); err != nil {
			return err
		}
	}
	return nil
}

func newestGoFile(root string, directories []string) (time.Time, error) {
	var newest time.Time
	for _, directory := range directories {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			return nil
		})
		if err != nil {
			return time.Time{}, fmt.Errorf("scan gateway sources: %w", err)
		}
	}
	return newest, nil
}

func buildGateway(root, architecture, output string) error {
	temporary, err := os.CreateTemp("", "llm-router-gateway-*")
	if err != nil {
		return fmt.Errorf("create gateway build file: %w", err)
	}
	binaryPath := temporary.Name()
	temporary.Close()
	defer os.Remove(binaryPath)

	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binaryPath, "./internal/dockergateway/cmd/gateway")
	command.Dir = root
	command.Env = EnvWith([]string{"GOOS=linux", "GOARCH=" + architecture, "CGO_ENABLED=0"})
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("go build gateway linux/%s: %w", architecture, err)
	}
	return gzipFile(binaryPath, output)
}

func gzipFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer input.Close()
	partial := destination + ".partial"
	file, err := os.Create(partial)
	if err != nil {
		return fmt.Errorf("create %s: %w", partial, err)
	}
	writer, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		file.Close()
		return err
	}
	if _, err := io.Copy(writer, input); err != nil {
		writer.Close()
		file.Close()
		return fmt.Errorf("compress gateway: %w", err)
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("compress gateway: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write %s: %w", partial, err)
	}
	return os.Rename(partial, destination)
}
