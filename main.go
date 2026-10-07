package main

import (
	"github.com/TheSlopMachine/llm-router/cmd"
	"github.com/TheSlopMachine/llm-router/internal/version"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// RouterVersion parses the stamped Version string. Unparsable values
// (e.g. dev) fall back to 0.0.0 display of the raw string by the caller.
var RouterVersion = version.Version{Major: 0, Minor: 0, Fix: 0}

func init() {
	if v, err := version.Parse(Version); err == nil {
		RouterVersion = v
	}
}

func main() {
	display := Version
	if v, err := version.Parse(Version); err == nil {
		display = v.String()
	}
	cmd.SetVersionInfo(display, GitCommit, BuildTime)
	cmd.Execute()
}
