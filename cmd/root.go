// Package cmd defines the llm-router CLI using cobra.
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/logging"
	"github.com/TheSlopMachine/llm-router/internal/server"
)

var (
	webPort            config.Port = 8080
	apiPort            config.Port = 8081
	databasePath       string
	sqlitePath         string
	noAuth             bool
	logLevel           config.LogLevel = config.LogLevelInfo
	devUIRedirect      string
	versionFlag        bool
	versionInformation struct {
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
	Use:   "llm-router [host]",
	Short: "llm-router — minimalist LLM routing gateway",
	Long:  "llm-router — minimalist LLM routing gateway",
	Example: `  llm-router
  llm-router localhost --web 8080 --api 8081 --db ./llm-router.db`,

	Args: cobra.MaximumNArgs(1),
	RunE: run,
}

// Execute is the entrypoint called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().Var(&webPort, "web", "port for dashboard UI")
	rootCmd.Flags().Var(&apiPort, "api", "port for /v1 OpenAI-compatible API")
	rootCmd.Flags().StringVar(&databasePath, "db", "llm-router.db", "path to the database file")
	rootCmd.Flags().StringVar(&sqlitePath, "sqlite-db", "", "path to the SQLite database file (defaults to the --db sibling)")
	rootCmd.Flags().BoolVar(&noAuth, "no-auth", false, "disable all authorization (dev and AI debugging only)")
	rootCmd.Flags().Var(&logLevel, "log-level", "log level: debug, info, warn, error")
	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "print version information and exit")
	rootCmd.Flags().StringVar(&devUIRedirect, "dev-ui-redirect", "", "internal: redirect dashboard navigations to this origin instead of serving the embedded SPA (used by `make start`)")
	_ = rootCmd.Flags().MarkHidden("dev-ui-redirect")
}

func run(cmd *cobra.Command, arguments []string) error {
	if versionFlag {
		fmt.Printf("llm-router version %s\n", versionInformation.Version)
		fmt.Printf("  commit: %s\n", versionInformation.GitCommit)
		fmt.Printf("  built:  %s\n", versionInformation.BuildTime)
		return nil
	}

	host := "localhost"
	if len(arguments) == 1 {
		host = arguments[0]
	}

	if webPort == apiPort {
		return fmt.Errorf("dashboard and api ports must differ (both %s)", webPort.String())
	}

	applicationConfig := &config.Config{
		DashboardAddr: net.JoinHostPort(host, webPort.String()),
		APIAddr:       net.JoinHostPort(host, apiPort.String()),
		DBPath:        databasePath,
		SQLitePath:    strings.TrimSpace(sqlitePath),
		LogLevel:      logLevel,
		NoAuth:        noAuth,
		DevUIRedirect: strings.TrimSpace(devUIRedirect),
	}

	// Logger
	logger := slog.New(logging.NewHandler(os.Stderr, logLevel.SlogLevel()))

	if applicationConfig.NoAuth {
		logger.Warn("authorization disabled (--no-auth): dev and AI debugging only")
	}

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
