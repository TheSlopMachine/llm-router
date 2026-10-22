package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/cli"
	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/i18n"
	"github.com/TheSlopMachine/llm-router/internal/instance"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   i18n.T("status.short"),
		Long:    i18n.T("status.long"),
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runStatus(true)
		},
	}
}

// runStatus prints the lock holder. Full status also asks the server over
// HTTP.
func runStatus(full bool) error {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	info, held, err := instance.Probe(lockPathFor(applicationConfig))
	if err != nil {
		return err
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer writer.Flush()
	if !held {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.status"), i18n.T("status.value.stopped"))
		return &exitError{code: notRunningExitCode}
	}
	fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.status"), i18n.T("status.value.running"))
	fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.mode"), info.Mode)
	fmt.Fprintf(writer, "%s\t%d\n", i18n.T("status.label.pid"), info.PID)
	if !info.Started.IsZero() {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.uptime"), cli.FormatDuration(time.Since(info.Started)))
	}
	if info.Address != "" {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.listen"), info.Address)
	}
	if info.Version != "" {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.version"), info.Version)
	}
	if !full || info.Address == "" {
		return nil
	}
	bootstrapped, reachable := queryBootstrapped(cli.DialAddress(info.Address))
	if !reachable {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.dashboard"), i18n.T("status.value.silent"))
		return nil
	}
	fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.dashboard"), i18n.T("status.value.answering"))
	if bootstrapped {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.password"), i18n.T("status.value.set"))
	} else {
		fmt.Fprintf(writer, "%s\t%s\n", i18n.T("status.label.password"), i18n.T("status.value.notSet"))
	}
	return nil
}

func queryBootstrapped(address string) (bool, bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + address + "/api/llm-router/status")
	if err != nil {
		return false, false
	}
	defer response.Body.Close()
	var payload struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&payload) != nil {
		return false, true
	}
	return payload.Bootstrapped, true
}
