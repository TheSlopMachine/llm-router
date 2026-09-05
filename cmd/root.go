// Package cmd defines the llm-router CLI using cobra.
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/server"
	"github.com/TheSlopMachine/llm-router/internal/util"
)

var (
	webPort              string
	apiPort              string
	dbPath               string
	testingKeyPath       string
	logLevel             string
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
  llm-router localhost --web 8080 --api 8081 --db ./llm-router.db
  llm-router 0.0.0.0 --web 3000 --api 3001 --db /var/lib/llm-router/data.db`,

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
	rootCmd.Flags().StringVar(&webPort, "web", "8080", "port for dashboard UI")
	rootCmd.Flags().StringVar(&apiPort, "api", "8081", "port for /v1 OpenAI-compatible API")
	rootCmd.Flags().StringVar(&dbPath, "db", "llm-router.db", "path to the bbolt database file")
	rootCmd.Flags().StringVar(&testingKeyPath, "testing-key", "", "path to file with ephemeral testing bearer token (generated if missing, not stored in DB)")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn, error")
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

	dashPort := webPort
	if dashPort == "" {
		dashPort = "8080"
	}
	if apiPort == "" {
		apiPort = "8081"
	}
	if dashPort == apiPort {
		return fmt.Errorf("dashboard and api ports must differ (both %s)", dashPort)
	}

	normalizedLogLevel := strings.ToLower(strings.TrimSpace(logLevel))
	switch normalizedLogLevel {
	case "", "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("invalid --log-level %q: must be one of debug, info, warn, error", logLevel)
	}
	if normalizedLogLevel == "warning" {
		normalizedLogLevel = "warn"
	}
	if normalizedLogLevel == "" {
		normalizedLogLevel = "info"
	}

	cfg := &config.Config{
		DashboardAddr:        fmt.Sprintf("%s:%s", host, dashPort),
		APIAddr:              fmt.Sprintf("%s:%s", host, apiPort),
		DBPath:               dbPath,
		LogLevel:             normalizedLogLevel,
		MaxCredentialRetries: maxCredentialRetries,
		TestingKeyPath:       testingKeyPath,
	}

	// Resolve testing key file (generate if missing)
	if testingKeyPath != "" {
		raw, err := ensureTestingKey(testingKeyPath)
		if err != nil {
			return fmt.Errorf("testing-key: %w", err)
		}
		cfg.TestingKey = raw
	}

	// Logger
	var level slog.Level
	switch normalizedLogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if cfg.TestingKey != "" {
		logger.Info("testing key enabled", "path", cfg.TestingKeyPath)
	}

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

func ensureTestingKey(path string) (string, error) {
	clean := filepath.Clean(path)

	// Ensure parent directory exists
	if dir := filepath.Dir(clean); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("create testing-key dir %q: %w", dir, err)
		}
	}

	// Try to read existing file
	data, err := os.ReadFile(clean)
	if err == nil {
		raw := strings.TrimSpace(string(data))
		if raw != "" {
			return raw, nil
		}
		// empty file → regenerate below
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read testing-key file %q: %w", clean, err)
	}

	// Generate new token
	raw, err := util.GenerateToken()
	if err != nil {
		return "", fmt.Errorf("generate testing token: %w", err)
	}
	if err := os.WriteFile(clean, []byte(raw+"\n"), 0600); err != nil {
		return "", fmt.Errorf("write testing-key file %q: %w", clean, err)
	}
	return raw, nil
}
