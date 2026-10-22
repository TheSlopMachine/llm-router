// Package i18n translates the text the command line prints for a person:
// help, errors and status labels. Event messages, JSON output, config
// values, command and flag names stay English because scripts and
// docs/EVENT-KEYS.md depend on their exact text.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Language is a supported output language.
type Language string

const (
	English Language = "en"
	Russian Language = "ru"
)

// languageVariable overrides the detected language.
const languageVariable = "LLM_ROUTER_LANG"

var current = English

// Detect picks the output language. LLM_ROUTER_LANG wins, then the first
// non-empty of LC_ALL, LC_MESSAGES and LANG, then the system locale. Russian
// is chosen only for a "ru" language code; every other value, including C
// and POSIX, gives English.
func Detect(lookup func(string) string, systemLocale string) Language {
	for _, name := range []string{languageVariable, "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := strings.TrimSpace(lookup(name)); value != "" {
			return fromLocale(value)
		}
	}
	return fromLocale(systemLocale)
}

func fromLocale(locale string) Language {
	code := strings.ToLower(strings.TrimSpace(locale))
	if cut := strings.IndexAny(code, "_.@-"); cut >= 0 {
		code = code[:cut]
	}
	if code == string(Russian) {
		return Russian
	}
	return English
}

// Init sets the language from the process environment.
func Init() {
	Set(Detect(os.Getenv, systemLocale()))
}

// Set selects the output language.
func Set(language Language) {
	current = language
}

// Current returns the selected language.
func Current() Language {
	return current
}

// T returns the text for key in the current language, formatted with
// arguments. A key missing from the language falls back to English; a key
// missing everywhere returns the key itself.
func T(key string, arguments ...any) string {
	text, found := Lookup(key)
	if !found {
		return key
	}
	if len(arguments) == 0 {
		return text
	}
	return fmt.Sprintf(text, arguments...)
}

// Lookup returns the unformatted text for key.
func Lookup(key string) (string, bool) {
	if text, found := catalogs[current][key]; found {
		return text, true
	}
	text, found := english[key]
	return text, found
}

var catalogs = map[Language]map[string]string{
	English: english,
	Russian: russian,
}

// Wrap returns err prefixed with the translated message for key.
func Wrap(err error, key string, arguments ...any) error {
	return fmt.Errorf("%s: %w", T(key, arguments...), err)
}
