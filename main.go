package main

import (
	"github.com/TheSlopMachine/llm-router/cmd"

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

