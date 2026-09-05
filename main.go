package main

import (
	"github.com/TheSlopMachine/llm-router/cmd"

	_ "github.com/TheSlopMachine/llm-router/internal/adapters/generic"
	_ "github.com/TheSlopMachine/llm-router/providers/agents"
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
