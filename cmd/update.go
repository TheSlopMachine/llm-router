package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/instance"
	"github.com/TheSlopMachine/llm-router/internal/updater"
	"github.com/TheSlopMachine/llm-router/internal/version"
)

const (
	backupSuffix       = ".pre-update.bak"
	stagedCheckTimeout = 15 * time.Second
)

var updateOptions struct {
	check bool
	force bool
}

func newUpdateCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "update",
		Short: "Update to the latest GitHub release",
		Long: `Update the binary to the latest release of ` + updater.Repository + `.

The archive is checked against the release checksums and the new binary
must report the release version before it replaces the old one. The old
binary stays next to it as llm-router.prev, and the database is copied to
<database>` + backupSuffix + ` (migrations do not roll back).

A server running in the background is stopped and started again on the new
binary. A server running in the foreground blocks the update: stop it
first. Containers are not updated in place: pull a new image.`,
		Example: `  llm-router update --check
  llm-router update`,
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		RunE:    runUpdate,
	}
	command.Flags().BoolVar(&updateOptions.check, "check", false, "only report whether an update exists")
	command.Flags().BoolVar(&updateOptions.force, "force", false, "allow a downgrade, a reinstall or an update of a dev build")
	return command
}

func runUpdate(command *cobra.Command, _ []string) error {
	ctx := command.Context()
	if inContainer() {
		return errors.New("update is disabled in containers: pull a new image instead")
	}
	current := versionInformation.Version
	currentVersion, currentErr := version.Parse(current)
	if currentErr != nil && !updateOptions.force && !updateOptions.check {
		return fmt.Errorf("build %q has no release version: use --force to install the latest release", current)
	}

	client := updater.Client{}
	release, err := client.Latest(ctx)
	if errors.Is(err, updater.ErrNoRelease) {
		return errors.New("no release is published yet")
	}
	if err != nil {
		return err
	}
	latestVersion, err := version.Parse(release.Tag)
	if err != nil {
		return fmt.Errorf("release tag %q is not a version: %w", release.Tag, err)
	}

	order := -1
	if currentErr == nil {
		order = updater.Compare(currentVersion, latestVersion)
	}
	if updateOptions.check {
		switch {
		case currentErr != nil:
			fmt.Printf("latest release: %s (build %q has no release version)\n", latestVersion, current)
		case order < 0:
			fmt.Printf("update available: %s -> %s\n", current, latestVersion)
		case order == 0:
			fmt.Printf("up to date (%s)\n", current)
		default:
			fmt.Printf("%s is newer than the latest release %s\n", current, latestVersion)
		}
		return nil
	}
	if order >= 0 && !updateOptions.force {
		if order == 0 {
			fmt.Printf("up to date (%s)\n", current)
		} else {
			fmt.Printf("%s is newer than the latest release %s: use --force to downgrade\n", current, latestVersion)
		}
		return nil
	}

	assetName := updater.AssetName(runtime.GOOS, runtime.GOARCH)
	assetURL, found := release.Assets[assetName]
	if !found {
		return fmt.Errorf("release %s has no %s", release.Tag, assetName)
	}
	checksumsURL, found := release.Assets[updater.ChecksumsAssetName]
	if !found {
		return fmt.Errorf("release %s has no %s: refusing to install an unverified binary", release.Tag, updater.ChecksumsAssetName)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	applicationConfig, _, err := config.Load()
	if err != nil {
		return err
	}
	lockPath := lockPathFor(applicationConfig)
	holder, held, err := instance.Probe(lockPath)
	if err != nil {
		return err
	}
	if held && holder.Mode != instance.ModeDaemon {
		return fmt.Errorf("a %s instance (pid %d) runs: stop it before updating", holder.Mode, holder.PID)
	}
	restartDaemon := held

	fmt.Printf("downloading %s\n", release.Tag)
	archive, err := client.Download(ctx, assetURL, updater.MaxArchiveBytes)
	if err != nil {
		return err
	}
	checksums, err := client.Download(ctx, checksumsURL, updater.MaxChecksumBytes)
	if err != nil {
		return err
	}
	if err := updater.VerifyChecksum(checksums, assetName, archive); err != nil {
		return err
	}
	binary, err := updater.ExtractBinary(archive, executableName())
	if err != nil {
		return err
	}
	staging, err := updater.Stage(executable, binary)
	if err != nil {
		return err
	}
	if err := verifyStagedBinary(ctx, staging, latestVersion.String()); err != nil {
		updater.Discard(staging)
		return err
	}

	if restartDaemon {
		if _, _, err := stopInstance(lockPath, daemonStopTimeout); err != nil {
			updater.Discard(staging)
			return err
		}
	}
	backupPath, err := backupDatabase(applicationConfig.DBPath)
	if err != nil {
		updater.Discard(staging)
		if restartDaemon {
			_, _ = startDaemon(applicationConfig, executable)
		}
		return err
	}
	previous, err := updater.Swap(executable, staging)
	if err != nil {
		updater.Discard(staging)
		if restartDaemon {
			_, _ = startDaemon(applicationConfig, executable)
		}
		return err
	}

	fmt.Printf("updated %s -> %s\n", current, latestVersion)
	fmt.Printf("previous binary: %s\n", previous)
	if backupPath != "" {
		fmt.Printf("database copy:   %s\n", backupPath)
	}
	if restartDaemon {
		info, err := startDaemon(applicationConfig, executable)
		if err != nil {
			return fmt.Errorf("the binary is updated, the server does not start: %w", err)
		}
		fmt.Printf("server restarted (pid %d)\n", info.PID)
	}
	return nil
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "llm-router.exe"
	}
	return "llm-router"
}

func inContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// verifyStagedBinary runs the new binary once. A wrong platform or a
// corrupt file fails here, before anything is replaced.
func verifyStagedBinary(ctx context.Context, staging, expectedVersion string) error {
	ctx, cancel := context.WithTimeout(ctx, stagedCheckTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, staging, "version").Output()
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run: %w", err)
	}
	if !strings.Contains(string(output), expectedVersion) {
		return fmt.Errorf("the downloaded binary reports %q, want version %s", strings.TrimSpace(string(output)), expectedVersion)
	}
	return nil
}

// backupDatabase copies the main database file, and its write-ahead log
// when a crash left one, next to the original. Only one copy is kept. The
// server is stopped by now, so the files are quiet.
func backupDatabase(databasePath string) (string, error) {
	if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	backupPath := databasePath + backupSuffix
	for _, suffix := range []string{"", "-wal"} {
		source := databasePath + suffix
		target := backupPath + suffix
		if err := copyFile(source, target); err != nil {
			if suffix == "-wal" && errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(target)
				continue
			}
			return "", fmt.Errorf("back up %s: %w", source, err)
		}
	}
	return backupPath, nil
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
