package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/cli"
	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/i18n"
)

var passwordOptions struct {
	stdin    bool
	value    string
	generate bool
	ifAbsent bool
}

func newPasswordCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "set-dashboard-password",
		Short: i18n.T("password.short"),
		Long:  i18n.T("password.long"),
		Example: `  llm-router set-dashboard-password
  llm-router set-dashboard-password --generate
  llm-router set-dashboard-password --password-stdin --if-absent < /run/secrets/admin_password`,
		GroupID: groupConfigure,
		Args:    cobra.NoArgs,
		RunE:    runSetDashboardPassword,
	}
	flags := command.Flags()
	flags.BoolVar(&passwordOptions.stdin, "password-stdin", false, i18n.T("password.flag.stdin"))
	flags.StringVar(&passwordOptions.value, "password", "", i18n.T("password.flag.value"))
	flags.BoolVar(&passwordOptions.generate, "generate", false, i18n.T("password.flag.generate"))
	flags.BoolVar(&passwordOptions.ifAbsent, "if-absent", false, i18n.T("password.flag.ifAbsent"))
	command.MarkFlagsMutuallyExclusive("password-stdin", "password", "generate")
	return command
}

func runSetDashboardPassword(command *cobra.Command, _ []string) error {
	if command.Flags().Changed("password") && passwordOptions.value == "" {
		return errors.New(i18n.T("password.empty"))
	}
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	ctx := command.Context()
	service, closeDatabase, err := openAdminService(ctx, applicationConfig)
	if err != nil {
		return err
	}
	defer closeDatabase()

	passwordSet, err := service.HasPassword(ctx)
	if err != nil {
		return i18n.Wrap(err, "password.check")
	}
	if passwordOptions.ifAbsent && passwordSet {
		fmt.Fprintln(os.Stderr, i18n.T("password.alreadySet"))
		return nil
	}

	password, err := obtainPassword(command)
	if err != nil {
		return err
	}
	if err := service.SetPassword(ctx, password); err != nil {
		return i18n.Wrap(err, "password.setFailed")
	}
	if passwordSet {
		fmt.Fprintln(os.Stderr, i18n.T("password.updated"))
	} else {
		fmt.Fprintln(os.Stderr, i18n.T("password.created"))
	}
	if passwordOptions.generate {
		fmt.Println(password)
	}
	return nil
}

func obtainPassword(command *cobra.Command) (string, error) {
	switch {
	case passwordOptions.generate:
		return cli.GeneratePassword()
	case command.Flags().Changed("password"):
		fmt.Fprintln(os.Stderr, i18n.T("password.warnArgument"))
		return passwordOptions.value, nil
	case passwordOptions.stdin:
		return cli.ReadPassword(os.Stdin)
	case isTerminal():
		return promptPasswordPair(i18n.T("password.prompt.new"), i18n.T("password.prompt.repeat"))
	default:
		return "", errors.New(i18n.T("password.noSource"))
	}
}
