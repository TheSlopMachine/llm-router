package i18n

import (
	"errors"
	"regexp"
	"sort"
	"testing"
)

func lookupFrom(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
		system string
		want   Language
	}{
		{"russian LANG", map[string]string{"LANG": "ru_RU.UTF-8"}, "", Russian},
		{"english LANG", map[string]string{"LANG": "en_US.UTF-8"}, "", English},
		{"no variables", map[string]string{}, "", English},
		{"C locale", map[string]string{"LANG": "C"}, "", English},
		{"POSIX locale", map[string]string{"LANG": "POSIX"}, "", English},
		{"unknown language", map[string]string{"LANG": "de_DE.UTF-8"}, "", English},
		{"override wins", map[string]string{"LLM_ROUTER_LANG": "en", "LANG": "ru_RU.UTF-8"}, "", English},
		{"override russian", map[string]string{"LLM_ROUTER_LANG": "ru", "LANG": "C"}, "", Russian},
		{"LC_ALL wins over LANG", map[string]string{"LC_ALL": "C", "LANG": "ru_RU.UTF-8"}, "", English},
		{"LC_MESSAGES before LANG", map[string]string{"LC_MESSAGES": "ru_RU", "LANG": "en_US"}, "", Russian},
		{"empty LC_ALL is skipped", map[string]string{"LC_ALL": "", "LANG": "ru_RU"}, "", Russian},
		{"windows locale", map[string]string{}, "ru-RU", Russian},
		{"environment before system", map[string]string{"LANG": "en_US"}, "ru-RU", English},
		{"bare code", map[string]string{"LANG": "ru"}, "", Russian},
		{"modifier", map[string]string{"LANG": "ru@euro"}, "", Russian},
		{"upper case", map[string]string{"LANG": "RU_RU"}, "", Russian},
	}
	for _, testCase := range cases {
		if got := Detect(lookupFrom(testCase.values), testCase.system); got != testCase.want {
			t.Errorf("%s: Detect = %s, want %s", testCase.name, got, testCase.want)
		}
	}
}

func TestCatalogsHaveSameKeys(t *testing.T) {
	var missing []string
	for key := range english {
		if _, found := russian[key]; !found {
			missing = append(missing, "ru: "+key)
		}
	}
	for key := range russian {
		if _, found := english[key]; !found {
			missing = append(missing, "en: "+key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("keys missing from one catalog: %v", missing)
	}
}

var verbPattern = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func TestCatalogsUseSameVerbs(t *testing.T) {
	for key, text := range english {
		translated, found := russian[key]
		if !found {
			continue
		}
		if first, second := verbPattern.FindAllString(text, -1), verbPattern.FindAllString(translated, -1); !equalStrings(first, second) {
			t.Errorf("%s: verbs differ: en %v, ru %v", key, first, second)
		}
	}
}

func equalStrings(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func TestTranslateAndFallback(t *testing.T) {
	defer Set(English)
	Set(Russian)
	if got := T("daemon.stopped", 7); got != "остановлен (pid 7)" {
		t.Errorf("ru T = %q", got)
	}
	Set(English)
	if got := T("daemon.stopped", 7); got != "stopped (pid 7)" {
		t.Errorf("en T = %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key = %q, want the key", got)
	}
}

func TestWrapKeepsCause(t *testing.T) {
	cause := errors.New("boom")
	wrapped := Wrap(cause, "doctor.fix")
	if !errors.Is(wrapped, cause) || wrapped.Error() != "fix: boom" {
		t.Errorf("Wrap = %v", wrapped)
	}
}
