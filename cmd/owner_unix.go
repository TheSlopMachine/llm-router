//go:build unix

package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
)

// runAsConfigurationOwner makes root act as the owner of the dotenv file.
// A package install gives the file to its service user. Database files
// that root creates then belong to that user, and the service can open
// them. A file owned by root, or no file, keeps root.
func runAsConfigurationOwner(*cobra.Command, []string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	path, err := config.ResolveDotenvPath()
	if err != nil {
		return err
	}
	information, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect dotenv file %s: %w", path, err)
	}
	status, ok := information.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("read the owner of %s", path)
	}
	if status.Uid == 0 {
		return nil
	}
	account, err := user.LookupId(strconv.FormatUint(uint64(status.Uid), 10))
	if err != nil {
		return fmt.Errorf("look up the owner of %s: %w", path, err)
	}
	groupID, err := strconv.Atoi(account.Gid)
	if err != nil {
		return fmt.Errorf("group of user %s: %w", account.Username, err)
	}
	// The group goes first: after Setuid the process may no longer change it.
	if err := syscall.Setgroups([]int{groupID}); err != nil {
		return fmt.Errorf("switch to user %s: %w", account.Username, err)
	}
	if err := syscall.Setgid(groupID); err != nil {
		return fmt.Errorf("switch to user %s: %w", account.Username, err)
	}
	if err := syscall.Setuid(int(status.Uid)); err != nil {
		return fmt.Errorf("switch to user %s: %w", account.Username, err)
	}
	return os.Setenv("HOME", account.HomeDir)
}
