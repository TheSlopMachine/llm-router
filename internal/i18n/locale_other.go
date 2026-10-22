//go:build !windows

package i18n

// systemLocale has no source beyond the environment on Unix systems.
func systemLocale() string { return "" }
