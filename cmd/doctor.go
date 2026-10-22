package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/i18n"
	"github.com/TheSlopMachine/llm-router/internal/instance"
	"github.com/TheSlopMachine/llm-router/internal/logging"
	"github.com/TheSlopMachine/llm-router/internal/server"
	"github.com/TheSlopMachine/llm-router/internal/services/doctor"
)

var doctorFix bool

func newDoctorCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "doctor",
		Short: i18n.T("doctor.short"),
		Long:  i18n.T("doctor.long"),
		Example: `  llm-router doctor
  llm-router doctor --fix`,
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		RunE:    runDoctor,
	}
	command.Flags().BoolVar(&doctorFix, "fix", false, i18n.T("doctor.flag.fix"))
	return command
}

func runDoctor(command *cobra.Command, _ []string) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	lock, err := acquireInstance(applicationConfig, instance.ModeMaintenance)
	if err != nil {
		return fmt.Errorf("%w; %s", err, i18n.T("doctor.stopFirst"))
	}
	defer lock.Release()

	ctx := command.Context()
	database, err := db.OpenSQLite(ctx, applicationConfig.DBPath, applicationConfig.CachePath, applicationConfig.EventsPath)
	if err != nil {
		return i18n.Wrap(err, "doctor.openDatabase")
	}
	defer database.Close()

	logger := slog.New(logging.NewHandler(os.Stderr, slog.LevelWarn))
	doctorService, err := server.NewDoctor(database, logger)
	if err != nil {
		return i18n.Wrap(err, "doctor.build")
	}
	report, err := doctorService.Inspect(ctx)
	if err != nil {
		return i18n.Wrap(err, "doctor.inspect")
	}
	if report.TotalIssues == 0 {
		fmt.Println(i18n.T("doctor.none"))
		return nil
	}
	printDoctorReport(report.Issues)
	if !doctorFix {
		return &exitError{code: 1}
	}
	fixed, err := doctorService.Fix(ctx, nil)
	if err != nil {
		return i18n.Wrap(err, "doctor.fix")
	}
	fmt.Println(i18n.T("doctor.fixed", fixed))
	report, err = doctorService.Inspect(ctx)
	if err != nil {
		return i18n.Wrap(err, "doctor.inspectAfter")
	}
	if report.TotalIssues > 0 {
		fmt.Println(i18n.T("doctor.remain", report.TotalIssues))
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
