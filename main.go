package main

import (
	"github.com/TheSlopMachine/llm-router/cmd"
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
