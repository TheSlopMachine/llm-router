package logging

import (
	"net/url"
	"regexp"
)

var (
	credentialAssignmentPattern = regexp.MustCompile(`(?i)((?:\b|["'])(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|auth(?:entication)?[_-]?token|client[_-]?secret|password|passwd|secret|authorization|cookie|set[_-]?cookie|token|key|credential)(?:\b|["'])\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}\]]+)`)
	bearerCredentialPattern     = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	basicCredentialPattern      = regexp.MustCompile(`(?i)\b(basic\s+)[A-Za-z0-9+/=]+`)
	credentialQueryPattern      = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|password|passwd|secret|authorization|token|key|credential)=)[^&#\s]+`)
	credentialURLPattern        = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	credentialTokenPattern      = regexp.MustCompile(`(?i)\b(?:sk|rk)-[A-Za-z0-9._~-]{16,}|\bgh[pousr]_[A-Za-z0-9._~-]{16,}|\bgithub_pat_[A-Za-z0-9._~-]{16,}|\bglpat-[A-Za-z0-9._~-]{16,}|\bxox[baprs]-[A-Za-z0-9._~-]{16,}|\bhf_[A-Za-z0-9._~-]{16,}|\bnpm_[A-Za-z0-9._~-]{16,}|\bpypi-[A-Za-z0-9._~-]{16,}|\bAIza[A-Za-z0-9_-]{20,}|\bya29\.[A-Za-z0-9._~-]{20,}|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)
)

var credentialKeyPattern = regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|auth(?:entication)?[_-]?token|client[_-]?secret|password|passwd|secret|authorization|cookie|set[_-]?cookie|(?:^|[._-])(?:token|key|credential)$)`)

func redactLogValue(key, value string) string {
	if credentialKeyPattern.MatchString(key) {
		return "[REDACTED]"
	}
	return redactLogText(value)
}

func redactLogText(value string) string {
	value = credentialURLPattern.ReplaceAllStringFunc(value, redactLogURL)
	value = credentialAssignmentPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = credentialQueryPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = bearerCredentialPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = basicCredentialPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	return credentialTokenPattern.ReplaceAllString(value, "[REDACTED]")
}

func redactLogURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User == nil {
		return value
	}
	return parsed.Redacted()
}
