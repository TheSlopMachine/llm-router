package main

import (
	"fmt"
	"os"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// help prints make usage. Display-only: reads the same env the other
// scripts use so shown defaults match real behavior.
func main() {
	host := shared.Getenv("HOST", "localhost")
	webPort := shared.Getenv("WEB_PORT", "8080")
	apiPort := shared.Getenv("API_PORT", "8081")
	url := shared.Getenv("URL", "http://"+host+":"+webPort)
	devDB := shared.Getenv("DEV_DB", shared.DefaultDevDB())
	devKey := shared.Getenv("DEV_KEY", shared.DefaultDevKey())

	fmt.Printf("\nUsage: make <target>\n\n")
	fmt.Printf("Targets:\n")
	fmt.Printf("  init              Initialize project (bun install, openapi.yaml, api-types, embed stub)\n")
	fmt.Printf("  start             Init, then start backend (go run) + frontend (vite) in dev mode\n")
	fmt.Printf("  stop              Stop dev processes\n")
	fmt.Printf("  restart           Stop + start\n")
	fmt.Printf("  status            Show dev server status\n")
	fmt.Printf("  browser           Open dashboard in browser (%s)\n", url)
	fmt.Printf("  clean             Stop + git clean -fdX (preserves untracked source)\n")
	fmt.Printf("  publish           Init, then build frontend + all PUBLISH_PLATFORMS binaries\n")
	fmt.Printf("  go-check          Run go vet\n")
	fmt.Printf("  go-test           Run go test ./...\n")
	fmt.Printf("  check-frontend    Run svelte-check (frontend type check)\n\n")
	fmt.Printf("Variables:\n")
	fmt.Printf("  HOST               Bind host. Default: \"%s\"\n", host)
	fmt.Printf("  WEB_PORT           Dashboard port. Default: \"%s\"\n", webPort)
	fmt.Printf("  API_PORT           API port. Default: \"%s\"\n", apiPort)
	fmt.Printf("  URL                Dashboard URL for browser. Default: \"http://$(HOST):$(WEB_PORT)\"\n")
	fmt.Printf("  DEV_DB             Path to dev database. Default: \"%s\"\n", devDB)
	fmt.Printf("  DEV_KEY            Path to dev testing key. Default: \"%s\"\n", devKey)
	fmt.Printf("  PUBLISH_PLATFORMS  Platforms for publish. Default: \"windows/amd64 ...\"\n")
	fmt.Printf("  NO_SKIP            Disable skipping of bun install + OpenAPI generation. Default: \"0\" (truthy: 1/true/yes/on)\n")
	{
		v := os.Getenv("NO_SKIP")
		note := "off"
		if shared.IsNoSkip() {
			note = "on"
		}
		if v == "" {
			v = "(unset)"
		}
		fmt.Printf("                     Current: NO_SKIP=%s (%s)\n\n", v, note)
	}
	fmt.Printf("Examples:\n")
	fmt.Printf("  make init\n")
	fmt.Printf("  make start\n")
	fmt.Printf("  make start HOST=0.0.0.0 WEB_PORT=3000 API_PORT=3001\n")
	fmt.Printf("  make restart\n")
	fmt.Printf("  make publish\n")
	fmt.Printf("  make publish PUBLISH_PLATFORMS=\"linux/amd64 darwin/arm64\"\n\n")
}
