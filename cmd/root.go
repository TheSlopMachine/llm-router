// Package cmd defines the llm-router CLI using cobra.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/logging"
	"github.com/TheSlopMachine/llm-router/internal/server"
)

// generateConfigurationBare marks a bare --generate-config flag (no path).
const generateConfigurationBare = "true"

var (
	versionFlag               bool
	generateConfigurationPath string
	bootstrapFlag             bool
	listLoginFlag             bool
	removeLoginFlag           bool
	resetLoginFlag            bool
	createLoginFlag           bool
	versionInformation        struct {
		Version   string
		GitCommit string
		BuildTime string
	}
)

// SetVersionInfo is called from main to inject build-time version info
func SetVersionInfo(version, commit, buildTime string) {
	versionInformation.Version = version
	versionInformation.GitCommit = commit
	versionInformation.BuildTime = buildTime
}

// rootCmd is the single CLI command — no sub-commands by design.
var rootCmd = &cobra.Command{
	Use:   "llm-router",
	Short: "llm-router — minimalist LLM routing gateway",
	Long:  "llm-router — minimalist LLM routing gateway (configuration from environment variables with optional .env file)",
	Example: `  llm-router
  llm-router --generate-config
  llm-router --bootstrap`,

	Args: cobra.NoArgs,
	RunE: run,
}

// Execute is the entrypoint called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "print version information and exit")
	rootCmd.Flags().StringVar(&generateConfigurationPath, "generate-config", "", "write default .env template to file and exit (bare flag uses ENV_FILE or .env)")
	rootCmd.Flags().Lookup("generate-config").NoOptDefVal = generateConfigurationBare
	rootCmd.Flags().BoolVar(&bootstrapFlag, "bootstrap", false, "create the initial admin login interactively and exit")
	rootCmd.Flags().BoolVar(&listLoginFlag, "list-login", false, "list admin logins and exit")
	rootCmd.Flags().BoolVar(&removeLoginFlag, "remove-login", false, "remove an admin login interactively and exit")
	rootCmd.Flags().BoolVar(&resetLoginFlag, "reset-login", false, "reset an admin login password interactively and exit")
	rootCmd.Flags().BoolVar(&createLoginFlag, "create-login", false, "create an admin login interactively and exit")
}

func run(command *cobra.Command, arguments []string) error {
	if versionFlag {
		fmt.Printf("llm-router version %s\n", versionInformation.Version)
		fmt.Printf("  commit: %s\n", versionInformation.GitCommit)
		fmt.Printf("  built:  %s\n", versionInformation.BuildTime)
		return nil
	}

	if command.Flags().Changed("generate-config") {
		target := strings.TrimSpace(generateConfigurationPath)
		if target == "" || target == generateConfigurationBare || target == "1" {
			target = config.ResolveDotenvPath()
		}
		slog.Default().Debug("Configuration template target resolves", "detail", target)
		target, err := config.ExpandPath(target)
		if err != nil {
			slog.Default().Error("Configuration template target fails to expand", "operation", "generate_config", "error", err)
			return fmt.Errorf("invalid generate-config target: %w", err)
		}
		if err := config.WriteDefaultFile(target); err != nil {
			slog.Default().Error("Configuration template fails to write", "operation", "generate_config", "detail", target, "error", err)
			if errors.Is(err, config.ErrConfigFileExists) {
				return err
			}
			return fmt.Errorf("write default configuration: %w", err)
		}
		slog.Default().Debug("Configuration template writes", "operation", "generate_config", "detail", target)
		fmt.Printf("wrote default configuration to %s\n", target)
		return nil
	}

	selectedLoginCommand, err := selectLoginCommand(bootstrapFlag, listLoginFlag, removeLoginFlag, resetLoginFlag, createLoginFlag)
	if err != nil {
		return err
	}
	switch selectedLoginCommand {
	case loginCommandBootstrap:
		return runBootstrap()
	case loginCommandList:
		return runListLogins()
	case loginCommandRemove:
		return runRemoveLogin()
	case loginCommandReset:
		return runResetLogin()
	case loginCommandCreate:
		return runCreateLogin()
	}

	applicationConfig, _, err := config.Load()
	if err != nil {
		slog.Default().Error("Configuration fails to load", "operation", "run_server", "error", err)
		return err
	}

	// Logger persists to the events database once the server wires it;
	// before that it renders stderr only.
	eventsHandler := logging.NewEventsHandler(os.Stderr, applicationConfig.LogLevel.SlogLevel(), nil)
	logger := slog.New(eventsHandler)
	logger.Debug("Configuration loads", "database", applicationConfig.DBPath, "cache", applicationConfig.CachePath, "events", applicationConfig.EventsPath)

	// Build server
	applicationServer, err := server.New(applicationConfig, logger)
	if err != nil {
		logger.Error("Server initialization fails", "operation", "run_server", "error", err)
		return fmt.Errorf("init server: %w", err)
	}
	eventsHandler.SetService(applicationServer.EventsService())
	defer applicationServer.Close()

	// Graceful shutdown on SIGINT / SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := applicationServer.Run(ctx); err != nil {
		logger.Error("Server run fails", "operation", "run_server", "error", err)
		return err
	}
	return nil
}
