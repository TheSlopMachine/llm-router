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
	"github.com/TheSlopMachine/llm-router/internal/instance"
	"github.com/TheSlopMachine/llm-router/internal/logging"
	"github.com/TheSlopMachine/llm-router/internal/server"
)

var serveLaunchedBy string

func newServeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the server in the foreground",
		Long: `Run the server in the foreground until SIGINT or SIGTERM.

Use this command in systemd units and containers. Configuration comes from
the environment and the .env file (see "llm-router config").`,
		Example: "  llm-router serve",
		GroupID: groupRun,
		Args:    cobra.NoArgs,
		RunE:    runServe,
	}
	command.Flags().StringVar(&serveLaunchedBy, "launched-by", "", "")
	_ = command.Flags().MarkHidden("launched-by")
	return command
}

func runServe(command *cobra.Command, _ []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		slog.Default().Error("Configuration fails to load", "operation", "run_server", "error", err)
		return err
	}

	mode := instance.ModeForeground
	if serveLaunchedBy == string(instance.ModeDaemon) {
		mode = instance.ModeDaemon
	}
	lock, err := acquireInstance(applicationConfig, mode)
	if err != nil {
		return err
	}
	defer lock.Release()

	// Logger persists to the events database once the server wires it;
	// before that it renders stderr only.
	eventsHandler := logging.NewEventsHandler(os.Stderr, applicationConfig.LogLevel.SlogLevel(), nil)
	logger := slog.New(eventsHandler)
	logger.Debug("Configuration loads", "database", applicationConfig.DBPath, "cache", applicationConfig.CachePath, "events", applicationConfig.EventsPath)

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
