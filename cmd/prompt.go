package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/TheSlopMachine/llm-router/internal/i18n"
)

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptLine prints a prompt and reads one line. Any read error aborts
// the command, including end of input.
func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", i18n.Wrap(err, "prompt.readInput")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptPasswordOnce reads one hidden password line. The terminal echoes
// nothing, so a newline follows the read.
func promptPasswordOnce(prompt string) (string, error) {
	fmt.Print(prompt)
	input, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", i18n.Wrap(err, "prompt.readPassword")
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
			fmt.Println(i18n.T("password.mustNotBeEmpty"))
			continue
		}
		confirm, err := promptPasswordOnce(confirmPrompt)
		if err != nil {
			return "", err
		}
		if password != confirm {
			fmt.Println(i18n.T("password.mismatch"))
			continue
		}
		return password, nil
	}
}
