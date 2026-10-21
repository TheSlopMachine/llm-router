package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/instance"
	"github.com/TheSlopMachine/llm-router/internal/logging"
	"github.com/TheSlopMachine/llm-router/internal/server"
	"github.com/TheSlopMachine/llm-router/internal/services/doctor"
)

var doctorFix bool

func newDoctorCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check the database and fix what can be fixed",
		Long: `Check the database for broken references and inconsistent records, the
same checks as the dashboard doctor.

The command needs the server stopped: it takes the database lock for the
whole run. Exit code 1 means issues remain.`,
		Example: `  llm-router doctor
  llm-router doctor --fix`,
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		RunE:    runDoctor,
	}
	command.Flags().BoolVar(&doctorFix, "fix", false, "apply every available fix")
	return command
}

func runDoctor(command *cobra.Command, _ []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	lock, err := acquireInstance(applicationConfig, instance.ModeMaintenance)
	if err != nil {
		return fmt.Errorf("%w; stop it first: llm-router daemon stop", err)
	}
	defer lock.Release()

	ctx := command.Context()
	database, err := db.OpenSQLite(ctx, applicationConfig.DBPath, applicationConfig.CachePath, applicationConfig.EventsPath)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer database.Close()

	logger := slog.New(logging.NewHandler(os.Stderr, slog.LevelWarn))
	doctorService, err := server.NewDoctor(database, logger)
	if err != nil {
		return fmt.Errorf("build doctor: %w", err)
	}
	report, err := doctorService.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}
	if report.TotalIssues == 0 {
		fmt.Println("no issues found")
		return nil
	}
	printDoctorReport(report.Issues)
	if !doctorFix {
		return &exitError{code: 1}
	}
	fixed, err := doctorService.Fix(ctx, nil)
	if err != nil {
		return fmt.Errorf("fix: %w", err)
	}
	fmt.Printf("fixed %d\n", fixed)
	report, err = doctorService.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("inspect after fix: %w", err)
	}
	if report.TotalIssues > 0 {
		fmt.Printf("%d issues remain:\n", report.TotalIssues)
		printDoctorReport(report.Issues)
		return &exitError{code: 1}
	}
	return nil
}

func printDoctorReport(issues []doctor.Issue) {
	for _, issue := range issues {
		fmt.Printf("%s (%d)\n", issue.Title, issue.Count)
		if description := strings.TrimSpace(issue.Description); description != "" {
			fmt.Printf("  %s\n", description)
		}
		if len(issue.Keys) > 0 {
			fmt.Printf("  %s\n", strings.Join(issue.Keys, ", "))
		}
	}
}
