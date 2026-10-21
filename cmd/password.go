package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/cli"
	"github.com/TheSlopMachine/llm-router/internal/config"
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
		Short: "Set the password of the dashboard user admin",
		Long: `Set the password of the dashboard user "admin". The user is created on
first use.

Without a source flag the command asks for the password twice on a terminal.
The change applies at once, no restart needed.

--if-absent leaves an existing password untouched. Use it in container
entrypoints: the password from a secret is only the first value, so a
password changed in the dashboard survives restarts. A password in
--password is visible in the process list and the shell history; prefer
--password-stdin.`,
		Example: `  llm-router set-dashboard-password
  llm-router set-dashboard-password --generate
  llm-router set-dashboard-password --password-stdin --if-absent < /run/secrets/admin_password`,
		GroupID: groupConfigure,
		Args:    cobra.NoArgs,
		RunE:    runSetDashboardPassword,
	}
	flags := command.Flags()
	flags.BoolVar(&passwordOptions.stdin, "password-stdin", false, "read the password from stdin")
	flags.StringVar(&passwordOptions.value, "password", "", "password as an argument (visible in ps and shell history)")
	flags.BoolVar(&passwordOptions.generate, "generate", false, "generate a random password and print it to stdout")
	flags.BoolVar(&passwordOptions.ifAbsent, "if-absent", false, "do nothing when the admin password is already set")
	command.MarkFlagsMutuallyExclusive("password-stdin", "password", "generate")
	return command
}

func runSetDashboardPassword(command *cobra.Command, _ []string) error {
	if command.Flags().Changed("password") && passwordOptions.value == "" {
		return errors.New("--password must not be empty")
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
		return fmt.Errorf("check dashboard accounts: %w", err)
	}
	users, err := service.ListLogins(ctx)
	if err != nil {
		return fmt.Errorf("list dashboard accounts: %w", err)
	}
	adminExists := false
	for _, user := range users {
		if user.Username == dashboardUsername {
			adminExists = true
		}
	}
	if passwordOptions.ifAbsent && bootstrapped && adminExists {
		fmt.Fprintln(os.Stderr, "admin password is already set")
		return nil
	}

	password, err := obtainPassword(command)
	if err != nil {
		return err
	}
	var outcome string
	switch {
	case !bootstrapped:
		err, outcome = service.Bootstrap(ctx, dashboardUsername, password), "created"
	case adminExists:
		err, outcome = service.ResetPassword(ctx, dashboardUsername, password), "updated"
	default:
		err, outcome = service.CreateLogin(ctx, dashboardUsername, password), "created"
	}
	if err != nil {
		return fmt.Errorf("set admin password: %w", err)
	}
	fmt.Fprintf(os.Stderr, "admin password %s\n", outcome)
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
		fmt.Fprintln(os.Stderr, "warning: a password in the command line is visible in the process list and the shell history")
		return passwordOptions.value, nil
	case passwordOptions.stdin:
		return cli.ReadPassword(os.Stdin)
	case isTerminal():
		return promptPasswordPair("New password: ", "Repeat password: ")
	default:
		return "", errors.New("no password source: use --password-stdin or --generate, or run in a terminal")
	}
}
