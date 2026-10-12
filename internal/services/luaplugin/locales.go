package luaplugin

import (
	"fmt"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// Supported widget locales. English is the fallback for missing keys.
const (
	LocaleEnglish = "en"
	LocaleRussian = "ru"
)

// Locale table bounds. Tables stay small because every key resolves one
// widget string.
const (
	maxLocaleKeys        = 1000
	maxLocaleKeyLength   = 128
	maxLocaleValueLength = 1000
)

// PluginLocales holds per-type widget translations:
// locale code -> widget key -> localized text.
type PluginLocales map[string]map[string]string

// NormalizeLocale maps a raw locale tag to the supported set.
// Russian tags resolve to ru, everything else resolves to en.
func NormalizeLocale(value string) string {
	tag := strings.ToLower(strings.TrimSpace(value))
	if tag == "" {
		return LocaleEnglish
	}
	if tag == LocaleRussian || strings.HasPrefix(tag, "ru-") || strings.HasPrefix(tag, "ru_") {
		return LocaleRussian
	}
	return LocaleEnglish
}

// NegotiateLocale selects the widget locale for one dashboard request.
// The explicit query value wins so the dashboard language override applies
// even when it differs from the browser header. The Accept-Language first
// tag serves as fallback. Empty input resolves to English.
func NegotiateLocale(queryValue, acceptLanguage string) string {
	if strings.TrimSpace(queryValue) != "" {
		return NormalizeLocale(queryValue)
	}
	header := strings.TrimSpace(acceptLanguage)
	if header == "" {
		return LocaleEnglish
	}
	first := header
	if index := strings.Index(first, ","); index >= 0 {
		first = first[:index]
	}
	if index := strings.Index(first, ";"); index >= 0 {
		first = first[:index]
	}
	return NormalizeLocale(first)
}

// ParseLocales validates one registration locales table.
// A nil value means the type declares no translations.
func ParseLocales(value lua.LValue, typeKey string) (PluginLocales, error) {
	if value == nil || value == lua.LNil {
		return nil, nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("plugin type %q: locales must be a table", typeKey)
	}
	out := PluginLocales{}
	var parseErr error
	table.ForEach(func(key, localeValue lua.LValue) {
		if parseErr != nil {
			return
		}
		code, ok := key.(lua.LString)
		if !ok {
			parseErr = fmt.Errorf("plugin type %q: locale code must be a string", typeKey)
			return
		}
		normalized := strings.ToLower(strings.TrimSpace(string(code)))
		if normalized != LocaleEnglish && normalized != LocaleRussian {
			parseErr = fmt.Errorf("plugin type %q: unsupported locale %q", typeKey, string(code))
			return
		}
		entries, ok := localeValue.(*lua.LTable)
		if !ok {
			parseErr = fmt.Errorf("plugin type %q: locale %q must be a table", typeKey, normalized)
			return
		}
		mapped := map[string]string{}
		entries.ForEach(func(entryKey, entryValue lua.LValue) {
			if parseErr != nil {
				return
			}
			keyString, ok := entryKey.(lua.LString)
			if !ok || strings.TrimSpace(string(keyString)) == "" {
				parseErr = fmt.Errorf("plugin type %q: locale %q keys must be non-empty strings", typeKey, normalized)
				return
			}
			valueString, ok := entryValue.(lua.LString)
			if !ok || strings.TrimSpace(string(valueString)) == "" {
				parseErr = fmt.Errorf("plugin type %q: locale %q value for %q must be a non-empty string", typeKey, normalized, string(keyString))
				return
			}
			if len(string(keyString)) > maxLocaleKeyLength {
				parseErr = fmt.Errorf("plugin type %q: locale %q key exceeds %d characters", typeKey, normalized, maxLocaleKeyLength)
				return
			}
			if len(string(valueString)) > maxLocaleValueLength {
				parseErr = fmt.Errorf("plugin type %q: locale %q value for %q exceeds %d characters", typeKey, normalized, string(keyString), maxLocaleValueLength)
				return
			}
			if len(mapped) >= maxLocaleKeys {
				parseErr = fmt.Errorf("plugin type %q: locale %q exceeds %d entries", typeKey, normalized, maxLocaleKeys)
				return
			}
			mapped[string(keyString)] = string(valueString)
		})
		if parseErr != nil {
			return
		}
		if existing, exists := out[normalized]; exists {
			for key, text := range mapped {
				existing[key] = text
			}
		} else {
			out[normalized] = mapped
		}
	})
	if parseErr != nil {
		return nil, parseErr
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// localizeText resolves one widget string through the locale tables.
// Exact matches win in the requested locale, then in English. Misses serve
// the stored text verbatim so old plugins without locales keep working.
func localizeText(text string, locales PluginLocales, locale string) string {
	if text == "" || len(locales) == 0 {
		return text
	}
	if table, ok := locales[locale]; ok {
		if translated, ok := table[text]; ok {
			return translated
		}
	}
	if locale != LocaleEnglish {
		if table, ok := locales[LocaleEnglish]; ok {
			if translated, ok := table[text]; ok {
				return translated
			}
		}
	}
	return text
}

// ResolveUINodes returns a copy of nodes with localizable strings resolved
// for locale. Stored trees stay untouched. Option values stay data;
// option labels resolve as display text.
func ResolveUINodes(nodes []*models.UINode, locales PluginLocales, locale string) []*models.UINode {
	normalized := NormalizeLocale(locale)
	if len(nodes) == 0 {
		return nodes
	}
	out := make([]*models.UINode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, resolveUINode(node, locales, normalized))
	}
	return out
}

func resolveUINode(node *models.UINode, locales PluginLocales, locale string) *models.UINode {
	if node == nil {
		return nil
	}
	resolved := *node
	resolved.Text = localizeText(node.Text, locales, locale)
	resolved.Label = localizeText(node.Label, locales, locale)
	resolved.Placeholder = localizeText(node.Placeholder, locales, locale)
	resolved.Title = localizeText(node.Title, locales, locale)
	resolved.Subtitle = localizeText(node.Subtitle, locales, locale)
	if len(node.OptionLabels) > 0 {
		labels := make(map[string]string, len(node.OptionLabels))
		for value, label := range node.OptionLabels {
			labels[value] = localizeText(label, locales, locale)
		}
		resolved.OptionLabels = labels
	}
	if len(node.Content) > 0 {
		content := make([]*models.UINode, 0, len(node.Content))
		for _, child := range node.Content {
			content = append(content, resolveUINode(child, locales, locale))
		}
		resolved.Content = content
	}
	return &resolved
}
