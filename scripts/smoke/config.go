package main

import (
	"fmt"
	"os"
	"strings"
)

// Valid SMOKE_TARGETS tokens: text-only by default, heavy on request.
var validTargets = map[string]bool{
	"completions": true,
	"messages":    true,
	"transcribe":  true,
	"speech":      true,
	"image":       true,
	"embeddings":  true,
}

type config struct {
	web     string
	api     string
	plugins []string
	targets map[string]bool
	store   string
	cleanup bool
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// baseURL builds http://host:port from the Makefile-exported HOST plus a
// port, unless an explicit SMOKE_* override is set.
func baseURL(override, port string) string {
	if v := os.Getenv(override); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	host := getenv("HOST", "localhost")
	return fmt.Sprintf("http://%s:%s", host, port)
}

func loadConfig() (config, error) {
	targets := map[string]bool{}
	for _, t := range strings.Split(getenv("SMOKE_TARGETS", "completions,messages"), ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if !validTargets[t] {
			return config{}, fmt.Errorf("unknown SMOKE_TARGETS entry %q (want completions,messages,transcribe,speech,image,embeddings)", t)
		}
		targets[t] = true
	}
	plugins := []string{}
	for _, p := range strings.Split(getenv("SMOKE_PLUGINS", "mock"), ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if p == "all" {
			plugins = []string{"mock", "groq", "google", "kiro", "opencode-zen", "opencode-free"}
			break
		}
		plugins = append(plugins, p)
	}
	return config{
		web:     baseURL("SMOKE_WEB", getenv("WEB_PORT", "38080")),
		api:     baseURL("SMOKE_API", getenv("API_PORT", "38081")),
		plugins: plugins,
		targets: targets,
		store:   getenv("SMOKE_STORE_DIR", "../../llm-router-store/llm-router-plugins"),
		cleanup: getenv("SMOKE_CLEANUP", "1") == "1",
	}, nil
}

// knownTypes lists runnable provider types. Unknown types fail fast: a
// typo in SMOKE_PLUGINS must be loud, never a silent skip.
var knownTypes = map[string]bool{
	"mock": true, "groq": true, "google": true,
	"kiro": true, "opencode-zen": true, "opencode-free": true,
}
