package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	host := flag.String("host", "localhost", "bind host")
	webPort := flag.String("web-port", "8080", "dashboard port")
	apiPort := flag.String("api-port", "8081", "api port")
	vitePort := flag.String("vite-port", "5173", "vite dev port")
	url := flag.String("url", "", "dashboard URL for browser")
	devDB := flag.String("dev-db", "", "path to dev database")
	devKey := flag.String("dev-key", "", "path to dev testing key")
	platforms := flag.String("platforms", "windows/amd64 windows/386 windows/arm64 linux/amd64 linux/386 linux/arm64 linux/arm darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/386 freebsd/arm64", "platforms for publish")
	remote := flag.String("remote", "https", "clone protocol for .workspace")
	flag.Parse()

	if *url == "" {
		*url = fmt.Sprintf("http://%s:%s", *host, *vitePort)
	}
	if *devDB == "" {
		if dir, err := shared.HomeLocal(); err == nil {
			*devDB = filepath.Join(dir, "llm-router-dev.db")
		} else {
			*devDB = "~/.local/llm-router/llm-router-dev.db"
		}
	}
	if *devKey == "" {
		if dir, err := shared.HomeLocal(); err == nil {
			*devKey = filepath.Join(dir, "llm-router-dev.key")
		} else {
			*devKey = "~/.local/llm-router/llm-router-dev.key"
		}
	}

	fmt.Printf("\nUsage: make <target>\n\n")
	fmt.Printf("Targets:\n")
	fmt.Printf("  start             Start backend (go run) + frontend (vite) in dev mode\n")
	fmt.Printf("  stop              Stop dev processes\n")
	fmt.Printf("  restart           Stop + start\n")
	fmt.Printf("  status            Show dev server status\n")
	fmt.Printf("  browser           Open dashboard in browser (%s)\n", *url)
	fmt.Printf("  clean             Stop + git clean -fdX (preserves untracked source)\n")
	fmt.Printf("  publish           Build frontend + workspace + all PUBLISH_PLATFORMS binaries\n")
	fmt.Printf("  go-check          Run go vet\n")
	fmt.Printf("  go-test           Run go test ./...\n")
	fmt.Printf("  check-frontend    Run svelte-check (frontend type check)\n\n")
	fmt.Printf("Variables:\n")
	fmt.Printf("  HOST               Bind host. Default: \"%s\"\n", *host)
	fmt.Printf("  WEB_PORT           Dashboard port. Default: \"%s\"\n", *webPort)
	fmt.Printf("  API_PORT           API port. Default: \"%s\"\n", *apiPort)
	fmt.Printf("  VITE_PORT          Vite dev server port. Default: \"%s\"\n", *vitePort)
	fmt.Printf("  URL                Dashboard URL for browser. Default: \"http://$(HOST):$(VITE_PORT)\"\n")
	fmt.Printf("  DEV_DB             Path to dev database. Default: \"%s\"\n", *devDB)
	fmt.Printf("  DEV_KEY            Path to dev testing key. Default: \"%s\"\n", *devKey)
	fmt.Printf("  PUBLISH_PLATFORMS  Platforms for publish. Default: \"windows/amd64 ...\"\n")
	fmt.Printf("  WORKSPACE_REMOTE   Clone protocol for .workspace. Default: \"%s\" (https|ssh)\n\n", *remote)
	// Keep platforms value visible for --help introspection without cluttering main output:
	_ = platforms
	fmt.Printf("Examples:\n")
	fmt.Printf("  make start\n")
	fmt.Printf("  make start HOST=0.0.0.0 WEB_PORT=3000 API_PORT=3001\n")
	fmt.Printf("  make restart\n")
	fmt.Printf("  make publish\n")
	fmt.Printf("  make publish PUBLISH_PLATFORMS=\"linux/amd64 darwin/arm64\"\n\n")
}
