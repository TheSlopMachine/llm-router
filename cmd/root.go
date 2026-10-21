// Package cmd defines the llm-router command line using cobra.
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	groupRun       = "run"
	groupConfigure = "configure"
	groupMaintain  = "maintain"
)

var versionInformation struct {
	Version   string
	GitCommit string
	BuildTime string
}

// SetVersionInfo is called from main to inject build-time version info.
func SetVersionInfo(version, commit, buildTime string) {
	versionInformation.Version = version
	versionInformation.GitCommit = commit
	versionInformation.BuildTime = buildTime
}

// exitError ends the process with a specific code. An empty message prints
// nothing: the command already reported the result.
type exitError struct {
	code    int
	message string
}

func (err *exitError) Error() string { return err.message }

var rootCommand = &cobra.Command{
	Use:           "llm-router",
	Short:         "llm-router — minimalist LLM routing gateway",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCommand.AddGroup(
		&cobra.Group{ID: groupRun, Title: "Run:"},
		&cobra.Group{ID: groupConfigure, Title: "Configure:"},
		&cobra.Group{ID: groupMaintain, Title: "Maintain:"},
	)
	rootCommand.SetHelpCommandGroupID(groupMaintain)
	rootCommand.SetCompletionCommandGroupID(groupMaintain)
	rootCommand.AddCommand(
		newServeCommand(),
		newDaemonCommand(),
		newLogsCommand(),
		newConfigCommand(),
		newPasswordCommand(),
		newStatusCommand(),
		newDoctorCommand(),
		newUpdateCommand(),
		newVersionCommand(),
	)
}

// Execute is the entrypoint called from main.
func Execute() {
	err := rootCommand.Execute()
	if err == nil {
		return
	}
	var exit *exitError
	if errors.As(err, &exit) {
		if exit.message != "" {
			fmt.Fprintf(os.Stderr, "llm-router: %s\n", exit.message)
		}
		os.Exit(exit.code)
	}
	fmt.Fprintf(os.Stderr, "llm-router: %v\n", err)
	os.Exit(1)
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print version, commit and build time",
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			fmt.Printf("llm-router version %s\n", versionInformation.Version)
			fmt.Printf("  commit: %s\n", versionInformation.GitCommit)
			fmt.Printf("  built:  %s\n", versionInformation.BuildTime)
		},
	}
}
