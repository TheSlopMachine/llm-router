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

// dashboardUsername is the only dashboard account. Only its password
// changes.
const dashboardUsername = "admin"

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

	bootstrapped, err := service.IsBootstrapped(ctx)
	if err != nil {
		return i18n.Wrap(err, "password.checkAccounts")
	}
	users, err := service.ListLogins(ctx)
	if err != nil {
		return i18n.Wrap(err, "password.listAccounts")
	}
	adminExists := false
	for _, user := range users {
		if user.Username == dashboardUsername {
			adminExists = true
		}
	}
	if passwordOptions.ifAbsent && bootstrapped && adminExists {
		fmt.Fprintln(os.Stderr, i18n.T("password.alreadySet"))
		return nil
	}

	password, err := obtainPassword(command)
	if err != nil {
		return err
	}
	var outcome string
	switch {
	case !bootstrapped:
		err, outcome = service.Bootstrap(ctx, dashboardUsername, password), "password.created"
	case adminExists:
		err, outcome = service.ResetPassword(ctx, dashboardUsername, password), "password.updated"
	default:
		err, outcome = service.CreateLogin(ctx, dashboardUsername, password), "password.created"
	}
	if err != nil {
		return i18n.Wrap(err, "password.setFailed")
	}
	fmt.Fprintln(os.Stderr, i18n.T(outcome))
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
