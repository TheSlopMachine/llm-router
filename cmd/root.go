// Package cmd defines the llm-router command line using cobra.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/i18n"
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

var rootCommand *cobra.Command

// init builds the command tree after the output language is known: every
// help text is translated when the command is created.
func init() {
	i18n.Init()
	rootCommand = &cobra.Command{
		Use:           "llm-router",
		Short:         i18n.T("root.short"),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCommand.AddGroup(
		&cobra.Group{ID: groupRun, Title: i18n.T("group.run")},
		&cobra.Group{ID: groupConfigure, Title: i18n.T("group.configure")},
		&cobra.Group{ID: groupMaintain, Title: i18n.T("group.maintain")},
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
	localizeBuiltIns()
}

// localizeBuiltIns translates what cobra creates itself: the help and
// completion commands, the -h flag of every command and the usage headers.
// Cobra's own error messages stay English.
func localizeBuiltIns() {
	rootCommand.InitDefaultHelpCmd()
	rootCommand.InitDefaultCompletionCmd()
	for _, command := range rootCommand.Commands() {
		switch command.Name() {
		case "help":
			command.Short = i18n.T("help.short")
		case "completion":
			command.Short = i18n.T("completion.short")
		}
	}
	var visit func(command *cobra.Command)
	visit = func(command *cobra.Command) {
		command.InitDefaultHelpFlag()
		if flag := command.Flags().Lookup("help"); flag != nil {
			flag.Usage = i18n.T("flag.help", command.Name())
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(rootCommand)
	rootCommand.SetUsageTemplate(localizedUsageTemplate(rootCommand.UsageTemplate()))
}

func localizedUsageTemplate(template string) string {
	return strings.NewReplacer(
		`Use "{{.CommandPath}} [command] --help" for more information about a command.`, i18n.T("usage.more"),
		"Usage:", i18n.T("usage.usage"),
		"Aliases:", i18n.T("usage.aliases"),
		"Examples:", i18n.T("usage.examples"),
		"Available Commands:", i18n.T("usage.commands"),
		"Additional Commands:", i18n.T("usage.additional"),
		"Global Flags:", i18n.T("usage.globalFlags"),
		"Flags:", i18n.T("usage.flags"),
		"Additional help topics:", i18n.T("usage.topics"),
		"[command]", i18n.T("usage.subcommand"),
	).Replace(template)
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
		Short:   i18n.T("version.short"),
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			fmt.Printf("llm-router version %s\n", versionInformation.Version)
			fmt.Printf("  commit: %s\n", versionInformation.GitCommit)
			fmt.Printf("  built:  %s\n", versionInformation.BuildTime)
		},
	}
}
