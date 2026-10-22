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
	"github.com/TheSlopMachine/llm-router/internal/i18n"
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
		Short: i18n.T("update.short"),
		Long:  i18n.T("update.long", updater.Repository, backupSuffix),
		Example: `  llm-router update --check
  llm-router update`,
		GroupID: groupMaintain,
		Args:    cobra.NoArgs,
		RunE:    runUpdate,
	}
	command.Flags().BoolVar(&updateOptions.check, "check", false, i18n.T("update.flag.check"))
	command.Flags().BoolVar(&updateOptions.force, "force", false, i18n.T("update.flag.force"))
	return command
}

func runUpdate(command *cobra.Command, _ []string) error {
	ctx := command.Context()
	if inContainer() {
		return errors.New(i18n.T("update.container"))
	}
	current := versionInformation.Version
	currentVersion, currentErr := version.Parse(current)
	if currentErr != nil && !updateOptions.force && !updateOptions.check {
		return errors.New(i18n.T("update.devBuild", current))
	}

	client := updater.Client{}
	release, err := client.Latest(ctx)
	if errors.Is(err, updater.ErrNoRelease) {
		return errors.New(i18n.T("update.noRelease"))
	}
	if err != nil {
		return err
	}
	latestVersion, err := version.Parse(release.Tag)
	if err != nil {
		return i18n.Wrap(err, "update.badTag", release.Tag)
	}

	order := -1
	if currentErr == nil {
		order = updater.Compare(currentVersion, latestVersion)
	}
	if updateOptions.check {
		switch {
		case currentErr != nil:
			fmt.Println(i18n.T("update.latestDev", latestVersion, current))
		case order < 0:
			fmt.Println(i18n.T("update.available", current, latestVersion))
		case order == 0:
			fmt.Println(i18n.T("update.upToDate", current))
		default:
			fmt.Println(i18n.T("update.newer", current, latestVersion))
		}
		return nil
	}
	if order >= 0 && !updateOptions.force {
		if order == 0 {
			fmt.Println(i18n.T("update.upToDate", current))
		} else {
			fmt.Println(i18n.T("update.newerForce", current, latestVersion))
		}
		return nil
	}

	assetName := updater.AssetName(runtime.GOOS, runtime.GOARCH)
	assetURL, found := release.Assets[assetName]
	if !found {
		return errors.New(i18n.T("update.noAsset", release.Tag, assetName))
	}
	checksumsURL, found := release.Assets[updater.ChecksumsAssetName]
	if !found {
		return errors.New(i18n.T("update.noChecksums", release.Tag, updater.ChecksumsAssetName))
	}
	executable, err := os.Executable()
	if err != nil {
		return i18n.Wrap(err, "daemon.resolve")
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
		return errors.New(i18n.T("update.running", holder.Mode, holder.PID))
	}
	restartDaemon := held

	fmt.Println(i18n.T("update.downloading", release.Tag))
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

	fmt.Println(i18n.T("update.done", current, latestVersion))
	fmt.Println(i18n.T("update.previous", previous))
	if backupPath != "" {
		fmt.Println(i18n.T("update.copy", backupPath))
	}
	if restartDaemon {
		info, err := startDaemon(applicationConfig, executable)
		if err != nil {
			return i18n.Wrap(err, "update.restartFailed")
		}
		fmt.Println(i18n.T("update.restarted", info.PID))
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
		return i18n.Wrap(err, "update.binaryNoRun")
	}
	if !strings.Contains(string(output), expectedVersion) {
		return errors.New(i18n.T("update.binaryVersion", strings.TrimSpace(string(output)), expectedVersion))
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
			return "", i18n.Wrap(err, "update.backup", source)
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
