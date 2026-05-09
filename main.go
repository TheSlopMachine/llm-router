//go:generate swag init --parseDependency --parseInternal --output build/docs

// @title           llm-router API
// @version         1.0
// @description     A zero-bloat, self-contained OpenAI-compatible LLM routing gateway.
// @description     Routes requests to registered provider backends with automatic credential management.

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and your router token.

// @securityDefinitions.apikey SessionAuth
// @in cookie
// @name llmr_session
// @description Session cookie for dashboard authentication.

package main

import (
	"github.com/TheSlopMachine/llm-router/cmd"

	// _ "github.com/TheSlopMachine/llm-router/build/docs"
	_ "github.com/TheSlopMachine/llm-router/providers/agents"
	_ "github.com/TheSlopMachine/llm-router/plugins"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	cmd.SetVersionInfo(Version, GitCommit, BuildTime)
	cmd.Execute()
}

