package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/instance"
)

var configInitForce bool

func newConfigCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Show and change the configuration",
		Long: `Show and change the deployment settings stored in the .env file.

Without a subcommand it opens an interactive menu. The file is ENV_FILE or
the default under the XDG config directory. Environment variables win over
the file; "config list" shows where each value comes from. Changes apply
after a restart of the server.`,
		GroupID: groupConfigure,
		Args:    cobra.NoArgs,
		RunE:    runConfigMenu,
	}
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Write a .env file with the default values",
		Args:  cobra.NoArgs,
		RunE:  runConfigInit,
	}
	initCommand.Flags().BoolVar(&configInitForce, "force", false, "overwrite an existing file")
	command.AddCommand(
		initCommand,
		&cobra.Command{
			Use:               "get KEY",
			Short:             "Print the effective value of a key",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: completeConfigKeys,
			RunE:              runConfigGet,
		},
		&cobra.Command{
			Use:               "set KEY VALUE",
			Short:             "Write a value to the .env file",
			Args:              cobra.ExactArgs(2),
			ValidArgsFunction: completeConfigKeys,
			RunE:              runConfigSet,
		},
		&cobra.Command{
			Use:               "unset KEY",
			Short:             "Remove a key from the .env file and restore its default",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: completeConfigKeys,
			RunE:              runConfigUnset,
		},
		&cobra.Command{
			Use:   "list",
			Short: "List all keys with value, source and meaning",
			Args:  cobra.NoArgs,
			RunE:  runConfigList,
		},
	)
	return command
}

func completeConfigKeys(_ *cobra.Command, arguments []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(arguments) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(config.Keys()))
	for _, key := range config.Keys() {
		names = append(names, key.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func dotenvPath() (string, error) {
	path, err := config.ExpandPath(config.ResolveDotenvPath())
	if err != nil {
		return "", fmt.Errorf("invalid ENV_FILE: %w", err)
	}
	return path, nil
}

func lookupConfigKey(name string) (config.Key, error) {
	key, found := config.LookupKey(name)
	if !found {
		return config.Key{}, fmt.Errorf("unknown key %q: see llm-router config list", name)
	}
	return key, nil
}

func runConfigInit(*cobra.Command, []string) error {
	path, err := dotenvPath()
	if err != nil {
		return err
	}
	if configInitForce {
		err = config.ReplaceDefaultFile(path)
	} else {
		err = config.WriteDefaultFile(path)
	}
	if errors.Is(err, config.ErrConfigFileExists) {
		return fmt.Errorf("%s exists: use --force to overwrite it", path)
	}
	if err != nil {
		return fmt.Errorf("write default configuration: %w", err)
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

func runConfigGet(_ *cobra.Command, arguments []string) error {
	key, err := lookupConfigKey(arguments[0])
	if err != nil {
		return err
	}
	path, err := dotenvPath()
	if err != nil {
		return err
	}
	settings, err := config.ResolveSettings(path)
	if err != nil {
		return err
	}
	for _, setting := range settings {
		if setting.Key.Name == key.Name {
			fmt.Println(setting.Value)
		}
	}
	return nil
}

func runConfigList(*cobra.Command, []string) error {
	path, err := dotenvPath()
	if err != nil {
		return err
	}
	settings, err := config.ResolveSettings(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "file %s\n", path)
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, setting := range settings {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", setting.Key.Name, setting.Value, setting.Source, setting.Key.Description)
	}
	return writer.Flush()
}

func runConfigSet(_ *cobra.Command, arguments []string) error {
	key, err := lookupConfigKey(arguments[0])
	if err != nil {
		return err
	}
	path, err := dotenvPath()
	if err != nil {
		return err
	}
	return assignSetting(path, key, arguments[1])
}

func runConfigUnset(_ *cobra.Command, arguments []string) error {
	key, err := lookupConfigKey(arguments[0])
	if err != nil {
		return err
	}
	path, err := dotenvPath()
	if err != nil {
		return err
	}
	return resetSetting(path, key)
}

// assignSetting validates and writes one value, then reports what the
// change needs to take effect.
func assignSetting(path string, key config.Key, raw string) error {
	normalized, err := key.Normalize(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key.Name, err)
	}
	settings, err := config.ResolveSettings(path)
	if err != nil {
		return err
	}
	values := make(map[string]string, len(settings))
	for _, setting := range settings {
		values[setting.Key.Name] = setting.Value
	}
	values[key.Name] = normalized
	if err := config.CheckDistinctPaths(values); err != nil {
		return err
	}
	if err := (config.DotenvFile{Path: path}).Set(key.Name, normalized); err != nil {
		return err
	}
	fmt.Printf("%s=%s\n", key.Name, normalized)
	return reportEffects(path, key)
}

func resetSetting(path string, key config.Key) error {
	removed, err := (config.DotenvFile{Path: path}).Unset(key.Name)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Printf("%s is not set in %s\n", key.Name, path)
		return nil
	}
	fmt.Printf("%s reset to default (%s)\n", key.Name, key.Default)
	return reportEffects(path, key)
}

// reportEffects warns about an environment override and about a running
// server that keeps the old value until restart.
func reportEffects(path string, key config.Key) error {
	settings, err := config.ResolveSettings(path)
	if err != nil {
		return err
	}
	var databasePath string
	for _, setting := range settings {
		if setting.Key.Name == key.Name && setting.Source == config.SourceEnvironment {
			fmt.Fprintf(os.Stderr, "%s is also set in the environment and wins over the file\n", key.Name)
		}
		if setting.Key.Name == "DB_PATH" {
			databasePath = setting.Value
		}
	}
	expanded, err := config.ExpandPath(databasePath)
	if err != nil {
		return nil
	}
	if _, held, err := instance.Probe(instance.LockPath(expanded)); err == nil && held {
		fmt.Fprintln(os.Stderr, "the server is running: restart it to apply the change")
	}
	return nil
}

// runConfigMenu lists the keys and edits one at a time: pick a number,
// type a value. An empty line leaves the menu.
func runConfigMenu(*cobra.Command, []string) error {
	if !isTerminal() {
		return errors.New("the menu needs a terminal: use config get, set, unset or list")
	}
	path, err := dotenvPath()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		settings, err := config.ResolveSettings(path)
		if err != nil {
			return err
		}
		fmt.Printf("\n%s\n\n", path)
		writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for index, setting := range settings {
			fmt.Fprintf(writer, "%d\t%s\t%s\t%s\n", index+1, setting.Key.Name, setting.Value, setting.Source)
		}
		_ = writer.Flush()
		fmt.Println()

		choice, err := promptLine(reader, "Number (empty to exit): ")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		choice = strings.TrimSpace(choice)
		if choice == "" {
			return nil
		}
		number, err := strconv.Atoi(choice)
		if err != nil || number < 1 || number > len(settings) {
			fmt.Println("no such item")
			continue
		}
		setting := settings[number-1]
		value, err := promptLine(reader, fmt.Sprintf("%s [%s] (empty keeps, - resets): ", setting.Key.Name, setting.Value))
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		value = strings.TrimSpace(value)
		switch value {
		case "":
			continue
		case "-":
			err = resetSetting(path, setting.Key)
		default:
			err = assignSetting(path, setting.Key, value)
		}
		if err != nil {
			fmt.Printf("error: %v\n", err)
		}
	}
}
