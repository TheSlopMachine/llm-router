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
  llm-router --generate-config`,

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
		if err := config.WriteDefaultFile(target); err != nil {
			if errors.Is(err, config.ErrConfigFileExists) {
				return err
			}
			return fmt.Errorf("write default configuration: %w", err)
		}
		fmt.Printf("wrote default configuration to %s\n", target)
		return nil
	}

	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}

	// Logger
	logger := slog.New(logging.NewHandler(os.Stderr, applicationConfig.LogLevel.SlogLevel()))

	// Build server
	applicationServer, err := server.New(applicationConfig, logger)
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}
	defer applicationServer.Close()

	// Graceful shutdown on SIGINT / SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return applicationServer.Run(ctx)
}
