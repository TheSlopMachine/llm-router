// Package cmd defines the llm-router CLI using cobra.
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/server"
)

var (
	listenAddr           string
	dashboardPort        string
	apiPort              string
	dbPath               string
	debug                bool
	maxCredentialRetries int
	versionFlag          bool
	versionInfo          struct {
		Version   string
		GitCommit string
		BuildTime string
	}
)

// SetVersionInfo is called from main to inject build-time version info
func SetVersionInfo(version, commit, buildTime string) {
	versionInfo.Version = version
	versionInfo.GitCommit = commit
	versionInfo.BuildTime = buildTime
}

// rootCmd is the single CLI command — no sub-commands by design.
var rootCmd = &cobra.Command{
	Use:   "llm-router [host]",
	Short: "A zero-bloat OpenAI-compatible LLM routing gateway",
	Long: `llm-router — minimalist LLM routing gateway

Routes OpenAI-compatible API requests to registered provider backends.
Manages provider credentials with automatic rotation.
Ships a lightweight HTMX dashboard for administration.
Dashboard and /v1 API run on separate ports.

Examples:
  llm-router localhost -p 8080 --api-port 8081 --db ./router.db
  llm-router 0.0.0.0 --dashboard-port 3000 --api-port 3001 --db /var/lib/llm-router/data.db`,

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
	rootCmd.Flags().StringVarP(&listenAddr, "port", "p", "8080", "port to listen on (dashboard, alias for --dashboard-port)")
	rootCmd.Flags().StringVar(&dashboardPort, "dashboard-port", "", "port for dashboard UI (default: value of --port)")
	rootCmd.Flags().StringVar(&apiPort, "api-port", "8081", "port for /v1 OpenAI-compatible API")
	rootCmd.Flags().StringVar(&dbPath, "db", "llm-router.db", "path to the bbolt database file")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "enable verbose debug logging")
	rootCmd.Flags().IntVar(&maxCredentialRetries, "max-retries", 7, "max credential rotation retry cycles (exponential backoff)")
	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "print version information and exit")
}

func run(cmd *cobra.Command, args []string) error {
	if versionFlag {
		fmt.Printf("llm-router version %s\n", versionInfo.Version)
		fmt.Printf("  commit: %s\n", versionInfo.GitCommit)
		fmt.Printf("  built:  %s\n", versionInfo.BuildTime)
		return nil
	}

	host := "localhost"
	if len(args) == 1 {
		host = args[0]
	}

	// Resolve dashboard port: --dashboard-port takes precedence over --port
	dashPort := dashboardPort
	if dashPort == "" {
		dashPort = listenAddr
	}
	if dashPort == "" {
		dashPort = "8080"
	}
	if apiPort == "" {
		apiPort = "8081"
	}
	if dashPort == apiPort {
		return fmt.Errorf("dashboard and api ports must differ (both %s)", dashPort)
	}

	cfg := &config.Config{
		DashboardAddr:        fmt.Sprintf("%s:%s", host, dashPort),
		APIAddr:              fmt.Sprintf("%s:%s", host, apiPort),
		ListenAddr:           fmt.Sprintf("%s:%s", host, dashPort), // compat
		DBPath:               dbPath,
		Debug:                debug,
		MaxCredentialRetries: maxCredentialRetries,
	}

	// Logger
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	// Build server
	srv, err := server.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}
	defer srv.Close()

	// Graceful shutdown on SIGINT / SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx)
}

