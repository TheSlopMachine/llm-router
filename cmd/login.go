package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/TheSlopMachine/llm-router/internal/config"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
)

// loginCommand selects one interactive account operation. The zero value
// runs the server: no login flag was set.
type loginCommand int

const (
	loginCommandNone loginCommand = iota
	loginCommandBootstrap
	loginCommandList
	loginCommandRemove
	loginCommandReset
	loginCommandCreate
)

// selectLoginCommand maps the login flag values onto one command. More
// than one set flag denies: account operations never combine.
func selectLoginCommand(bootstrap, listLogins, removeLogin, resetLogin, createLogin bool) (loginCommand, error) {
	candidates := []struct {
		command loginCommand
		set     bool
	}{
		{loginCommandBootstrap, bootstrap},
		{loginCommandList, listLogins},
		{loginCommandRemove, removeLogin},
		{loginCommandReset, resetLogin},
		{loginCommandCreate, createLogin},
	}
	selected := loginCommandNone
	count := 0
	for _, candidate := range candidates {
		if !candidate.set {
			continue
		}
		count++
		selected = candidate.command
	}
	if count > 1 {
		return loginCommandNone, fmt.Errorf("only one of --bootstrap, --list-login, --remove-login, --reset-login, --create-login may be set")
	}
	return selected, nil
}

// requireInteractiveTerminal fails when stdin is not a terminal. Every
// login flag is interactive only: piped or redirected stdin never feeds
// passwords or confirmations.
func requireInteractiveTerminal() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("login management is interactive only: stdin is not a terminal")
	}
	return nil
}

// openAdminService loads configuration and opens the SQLite files,
// creating and migrating them on first run. The caller closes the
// database. No listener binds: account commands never serve traffic.
func openAdminService() (*admin.GormService, *db.Database, error) {
	applicationConfig, _, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	sqliteDatabase, err := db.OpenSQLite(context.Background(), applicationConfig.DBPath, applicationConfig.CachePath, applicationConfig.EventsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite database: %w", err)
	}
	return admin.NewGorm(sqliteDatabase), sqliteDatabase, nil
}

// promptLine prints a prompt and reads one line. Any read error aborts
// the operation: callers never loop on I/O failures.
func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptUsername reads a login name, re-prompting while it trims empty.
func promptUsername(reader *bufio.Reader) (string, error) {
	for {
		line, err := promptLine(reader, "Login name: ")
		if err != nil {
			return "", err
		}
		username := strings.TrimSpace(line)
		if username == "" {
			fmt.Println("login name must not be empty")
			continue
		}
		return username, nil
	}
}

// promptPasswordOnce reads one hidden password line. The terminal echoes
// nothing, so a newline follows the read.
func promptPasswordOnce(prompt string) (string, error) {
	fmt.Print(prompt)
	input, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(input), nil
}

// promptPasswordPair reads a hidden password with confirmation. An empty
// password re-prompts; a mismatch restarts at the first prompt.
func promptPasswordPair(firstPrompt, confirmPrompt string) (string, error) {
	for {
		password, err := promptPasswordOnce(firstPrompt)
		if err != nil {
			return "", err
		}
		if password == "" {
			fmt.Println("password must not be empty")
			continue
		}
		confirm, err := promptPasswordOnce(confirmPrompt)
		if err != nil {
			return "", err
		}
		if password != confirm {
			fmt.Println("passwords do not match")
			continue
		}
		return password, nil
	}
}

// promptLoginCredentials reads a login name plus a confirmed hidden
// password. Bootstrap and creation share it.
func promptLoginCredentials(reader *bufio.Reader) (string, string, error) {
	username, err := promptUsername(reader)
	if err != nil {
		return "", "", err
	}
	password, err := promptPasswordPair("Password: ", "Confirm password: ")
	if err != nil {
		return "", "", err
	}
	return username, password, nil
}

// confirmRemoval asks for explicit confirmation. An empty answer denies;
// anything but y/yes/n/no re-prompts.
func confirmRemoval(reader *bufio.Reader, username string) (bool, error) {
	for {
		line, err := promptLine(reader, fmt.Sprintf("Remove login %q? [y/N]: ", username))
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			fmt.Println("answer y or n")
		}
	}
}

// runBootstrap creates the initial admin login and marks the instance
// bootstrapped. An already bootstrapped instance denies before prompting.
func runBootstrap() error {
	if err := requireInteractiveTerminal(); err != nil {
		return err
	}
	service, sqliteDatabase, err := openAdminService()
	if err != nil {
		return err
	}
	defer sqliteDatabase.Close()
	ctx := context.Background()
	bootstrapped, err := service.IsBootstrapped(ctx)
	if err != nil {
		return err
	}
	if bootstrapped {
		return fmt.Errorf("already bootstrapped")
	}
	reader := bufio.NewReader(os.Stdin)
	username, password, err := promptLoginCredentials(reader)
	if err != nil {
		return err
	}
	if err := service.Bootstrap(ctx, username, password); err != nil {
		return err
	}
	fmt.Printf("created login %q\n", username)
	return nil
}

// runCreateLogin adds an operator account to an already bootstrapped
// instance. The service enforces the bootstrapped state; the pre-check
// skips pointless prompting.
func runCreateLogin() error {
	if err := requireInteractiveTerminal(); err != nil {
		return err
	}
	service, sqliteDatabase, err := openAdminService()
	if err != nil {
		return err
	}
	defer sqliteDatabase.Close()
	ctx := context.Background()
	bootstrapped, err := service.IsBootstrapped(ctx)
	if err != nil {
		return err
	}
	if !bootstrapped {
		return fmt.Errorf("system not bootstrapped: run --bootstrap first")
	}
	reader := bufio.NewReader(os.Stdin)
	username, password, err := promptLoginCredentials(reader)
	if err != nil {
		return err
	}
	if err := service.CreateLogin(ctx, username, password); err != nil {
		return err
	}
	fmt.Printf("created login %q\n", username)
	return nil
}

// runListLogins prints one username per line. The output carries no
// other fields, so it stays pipeable.
func runListLogins() error {
	if err := requireInteractiveTerminal(); err != nil {
		return err
	}
	service, sqliteDatabase, err := openAdminService()
	if err != nil {
		return err
	}
	defer sqliteDatabase.Close()
	users, err := service.ListLogins(context.Background())
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("no admin accounts")
		return nil
	}
	for _, user := range users {
		fmt.Println(user.Username)
	}
	return nil
}

// runRemoveLogin deletes one operator account after confirmation. A
// declined confirmation cancels without touching storage.
func runRemoveLogin() error {
	if err := requireInteractiveTerminal(); err != nil {
		return err
	}
	service, sqliteDatabase, err := openAdminService()
	if err != nil {
		return err
	}
	defer sqliteDatabase.Close()
	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)
	username, err := promptUsername(reader)
	if err != nil {
		return err
	}
	confirmed, err := confirmRemoval(reader, username)
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("removal cancelled")
		return nil
	}
	if err := service.DeleteUser(ctx, username); err != nil {
		return err
	}
	fmt.Printf("removed login %q\n", username)
	return nil
}

// runResetLogin stores a new password for one operator account. The
// current password is not asked: local database access is
// root-equivalent.
func runResetLogin() error {
	if err := requireInteractiveTerminal(); err != nil {
		return err
	}
	service, sqliteDatabase, err := openAdminService()
	if err != nil {
		return err
	}
	defer sqliteDatabase.Close()
	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)
	username, err := promptUsername(reader)
	if err != nil {
		return err
	}
	password, err := promptPasswordPair("New password: ", "Confirm new password: ")
	if err != nil {
		return err
	}
	if err := service.ResetPassword(ctx, username, password); err != nil {
		return err
	}
	fmt.Printf("reset password for login %q\n", username)
	return nil
}
