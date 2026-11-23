package cmd

import "github.com/spf13/cobra"

// runAsConfigurationOwner has nothing to do on Windows: packages install no
// service there, so no file belongs to a service account.
func runAsConfigurationOwner(*cobra.Command, []string) error {
	return nil
}
