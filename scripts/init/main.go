package main

import (
	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// init prepares the project for `make start` / `make publish`:
// bun install, web/openapi.yaml, TypeScript API types, embed stub.
// Steps whose outputs are newer than their inputs are skipped,
// unless NO_SKIP is truthy. Configuration comes from env only.
func main() {
	root, err := shared.RootDir()
	if err != nil {
		shared.Failf("%v", err)
	}

	host := shared.Getenv("HOST", "localhost")
	webPort := shared.Getenv("WEB_PORT", "8080")
	noSkip := shared.IsNoSkip()

	if err := shared.EnsureBunInstall(root, noSkip); err != nil {
		shared.Failf("%v", err)
	}

	// OpenAPI generation is strict: swag must succeed.
	if err := shared.RunSwag(root, noSkip); err != nil {
		shared.Failf("%v", err)
	}

	// TypeScript types: strict.
	if err := shared.RunAPITypes(root, noSkip); err != nil {
		shared.Failf("%v", err)
	}

	if err := shared.EnsureEmbedStub(root, host, webPort); err != nil {
		shared.Failf("%v", err)
	}

	shared.OKf("Project initialized (dev)")
}
