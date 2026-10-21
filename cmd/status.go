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
	"github.com/TheSlopMachine/llm-router/internal/instance"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the state of the running server",
		Long: `Show the state of the running server: process, uptime, address, version
and whether the dashboard answers and has a password.

Exit code 3 means the server is not running.`,
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
		fmt.Fprintln(writer, "status\tstopped")
		return &exitError{code: notRunningExitCode}
	}
	fmt.Fprintln(writer, "status\trunning")
	fmt.Fprintf(writer, "mode\t%s\n", info.Mode)
	fmt.Fprintf(writer, "pid\t%d\n", info.PID)
	if !info.Started.IsZero() {
		fmt.Fprintf(writer, "uptime\t%s\n", cli.FormatDuration(time.Since(info.Started)))
	}
	if info.Address != "" {
		fmt.Fprintf(writer, "listen\t%s\n", info.Address)
	}
	if info.Version != "" {
		fmt.Fprintf(writer, "version\t%s\n", info.Version)
	}
	if !full || info.Address == "" {
		return nil
	}
	bootstrapped, reachable := queryBootstrapped(cli.DialAddress(info.Address))
	if !reachable {
		fmt.Fprintln(writer, "dashboard\tnot answering")
		return nil
	}
	fmt.Fprintln(writer, "dashboard\tanswering")
	if bootstrapped {
		fmt.Fprintln(writer, "password\tset")
	} else {
		fmt.Fprintln(writer, "password\tnot set (run: llm-router set-dashboard-password)")
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
