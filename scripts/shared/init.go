package shared

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EnsureBunInstall runs `bun install` in web/ unless node_modules is newer
// than bun.lock + package.json. noSkip forces the install.
func EnsureBunInstall(root string, noSkip bool) error {
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
		if noSkip {
			Stepf("NO_SKIP=1: forcing bun install")
		} else {
			Stepf("bun install: up to date, skip")
			return nil
		}
	}
	Stepf("Running bun install...")
	cmd := exec.Command("bun", "install")
	cmd.Dir = webDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bun install: %w", err)
	}
	return nil
}

// RunSwag regenerates web/openapi.yaml from Go annotations unless it is
// newer than all internal/**/*.go. noSkip forces regeneration.
func RunSwag(root string, noSkip bool) error {
	if !noSkip && OpenAPIUpToDate(root) {
		Stepf("openapi.yaml up to date, skip swag")
		return nil
	}
	Stepf("Generating OpenAPI spec from Go annotations...")
	webDir := filepath.Join(root, "web")
	cmd := exec.Command("go", "run", "github.com/swaggo/swag/cmd/swag@v1.16.4",
		"init", "-g", "internal/dashboard/handler.go",
		"-o", webDir, "--parseDependency", "--parseInternal", "--parseDepth", "2",
		"--outputTypes", "yaml", "--quiet")
	cmd.Dir = root
	cmd.Env = EnvWithoutGowork()
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

// RunAPITypes regenerates web/src/lib/generated/api-types.ts unless it is
// newer than web/openapi.yaml. noSkip forces regeneration.
func RunAPITypes(root string, noSkip bool) error {
	if !noSkip && APITypesUpToDate(root) {
		Stepf("api-types.ts up to date, skip")
		return nil
	}
	Stepf("Generating TypeScript API types...")
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

// OpenAPIUpToDate reports whether web/openapi.yaml is newer than every Go
// file under internal/.
func OpenAPIUpToDate(root string) bool {
	openapiYAML := filepath.Join(root, "web", "openapi.yaml")
	st, err := os.Stat(openapiYAML)
	if err != nil {
		return false
	}
	cutoff := st.ModTime()
	internalDir := filepath.Join(root, "internal")
	upToDate := true
	_ = filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			upToDate = false
		}
		return nil
	})
	return upToDate
}

// APITypesUpToDate reports whether api-types.ts is newer than openapi.yaml.
func APITypesUpToDate(root string) bool {
	openapiYAML := filepath.Join(root, "web", "openapi.yaml")
	out := filepath.Join(root, "web", "src", "lib", "generated", "api-types.ts")
	stOpenapi, err1 := os.Stat(openapiYAML)
	stOut, err2 := os.Stat(out)
	if err1 != nil || err2 != nil {
		return false
	}
	return !stOpenapi.ModTime().After(stOut.ModTime())
}

// EnsureEmbedStub creates the dev placeholder for the //go:embed directive
// when missing. Never overwrites an existing file.
func EnsureEmbedStub(root, host, vitePort string) error {
	buildWeb := filepath.Join(root, "internal", "dashboard", "build", "web")
	if err := os.MkdirAll(buildWeb, 0755); err != nil {
		return fmt.Errorf("create build/web: %w", err)
	}
	indexPath := filepath.Join(buildWeb, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return nil
	}
	Stepf("Creating embed stub %s...", indexPath)
	devURL := fmt.Sprintf("http://%s:%s", host, vitePort)
	html := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>llm-router dev</title></head><body style="font-family:system-ui;padding:40px"><h1>llm-router dev placeholder</h1><p>This file only exists to satisfy the //go:embed directive in internal/dashboard/handler.go during dev builds — it is not the real UI.</p><p>You're seeing it because the backend was started without <code>--dev-ui-redirect</code>. Via <code>make start</code> it redirects here automatically to <a href="%s">%s</a> instead.</p></body></html>`, devURL, devURL)
	if _, err := WriteIfChanged(indexPath, []byte(html), 0644); err != nil {
		return err
	}
	return nil
}
