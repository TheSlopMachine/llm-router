package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	mode := flag.String("mode", "dev", "dev or build")
	host := flag.String("host", "localhost", "host for dev placeholder")
	vitePort := flag.String("vite-port", "5173", "vite port for dev placeholder")
	flag.Parse()
	if *mode != "dev" && *mode != "build" {
		shared.Failf("invalid --mode %q: must be dev or build", *mode)
	}

	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	if err := ensureBunInstall(root); err != nil {
		shared.Failf("%v", err)
	}

	// OpenAPI generation is strict: swag must succeed.
	if err := runSwag(root); err != nil {
		shared.Failf("%v", err)
	}

	// TypeScript types: strict.
	if err := runAPITypes(root); err != nil {
		shared.Failf("%v", err)
	}

	if err := ensureEmbedStub(root, *host, *vitePort); err != nil {
		shared.Failf("%v", err)
	}

	if *mode == "build" {
		if err := runViteBuild(root); err != nil {
			shared.Failf("%v", err)
		}
		shared.OKf("Frontend ready (build)")
	} else {
		shared.OKf("Frontend ready (dev)")
	}
}

func ensureBunInstall(root string) error {
	webDir := filepath.Join(root, "web")
	lockFile := filepath.Join(webDir, "bun.lock")
	pkgFile := filepath.Join(webDir, "package.json")
	nodeModules := filepath.Join(webDir, "node_modules")

	need := false
	if st, err := os.Stat(nodeModules); err != nil || !st.IsDir() {
		need = true
	} else {
		modTime := st.ModTime()
		for _, p := range []string{lockFile, pkgFile} {
			if st2, err := os.Stat(p); err == nil && st2.ModTime().After(modTime) {
				need = true
				break
			}
		}
	}
	if !need {
		shared.Stepf("bun install: up to date, skip")
		return nil
	}
	shared.Stepf("Running bun install...")
	cmd := exec.Command("bun", "install")
	cmd.Dir = webDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bun install: %w", err)
	}
	return nil
}

func runSwag(root string) error {
	shared.Stepf("Generating OpenAPI spec from Go annotations...")
	webDir := filepath.Join(root, "web")
	cmd := exec.Command("go", "run", "github.com/swaggo/swag/cmd/swag@v1.16.4",
		"init", "-g", "internal/dashboard/handler.go",
		"-o", webDir, "--parseDependency", "--parseInternal", "--parseDepth", "2",
		"--outputTypes", "yaml", "--quiet")
	cmd.Dir = root
	cmd.Env = shared.EnvWithoutGowork()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("swag init: %w", err)
	}

	// swag writes swagger.yaml in some versions; normalize to openapi.yaml
	swaggerYAML := filepath.Join(webDir, "swagger.yaml")
	openapiYAML := filepath.Join(webDir, "openapi.yaml")
	if _, err := os.Stat(swaggerYAML); err == nil {
		if err := os.Rename(swaggerYAML, openapiYAML); err != nil {
			return fmt.Errorf("rename swagger.yaml: %w", err)
		}
	}
	// Remove swag byproducts.
	_ = os.Remove(filepath.Join(webDir, "swagger.json"))
	_ = os.Remove(filepath.Join(webDir, "docs.go"))

	if _, err := os.Stat(openapiYAML); err != nil {
		return fmt.Errorf("openapi.yaml not generated at %s: %w", openapiYAML, err)
	}
	return nil
}

func runAPITypes(root string) error {
	shared.Stepf("Generating TypeScript API types...")
	webDir := filepath.Join(root, "web")
	cmd := exec.Command("bun", "run", "generate:api-types")
	cmd.Dir = webDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate:api-types: %w", err)
	}
	out := filepath.Join(webDir, "src", "lib", "generated", "api-types.ts")
	if _, err := os.Stat(out); err != nil {
		return fmt.Errorf("api-types.ts not generated at %s: %w", out, err)
	}
	// Bump mtime to satisfy freshness checks.
	now := time.Now()
	_ = os.Chtimes(out, now, now)
	return nil
}

func ensureEmbedStub(root, host, vitePort string) error {
	buildWeb := filepath.Join(root, "internal", "dashboard", "build", "web")
	if err := os.MkdirAll(buildWeb, 0755); err != nil {
		return fmt.Errorf("create build/web: %w", err)
	}
	indexPath := filepath.Join(buildWeb, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return nil
	}
	shared.Stepf("Creating embed stub %s...", indexPath)
	devURL := fmt.Sprintf("http://%s:%s", host, vitePort)
	html := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>llm-router dev</title></head><body style="font-family:system-ui;padding:40px"><h1>llm-router dev placeholder</h1><p>This file only exists to satisfy the //go:embed directive in internal/dashboard/handler.go during dev builds — it is not the real UI.</p><p>You're seeing it because the backend was started without <code>--dev-ui-redirect</code>. Via <code>make start</code> it redirects here automatically to <a href="%s">%s</a> instead.</p></body></html>`, devURL, devURL)
	if _, err := shared.WriteIfChanged(indexPath, []byte(html), 0644); err != nil {
		return err
	}
	return nil
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
