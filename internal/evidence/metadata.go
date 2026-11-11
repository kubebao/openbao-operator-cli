package evidence

import (
	"regexp"
	"strings"
	"unicode"
)

var bearerText = regexp.MustCompile(`(?i)\bbearer\s+[^\s"',;]+`)

var sensitiveText = regexp.MustCompile(
	`(?i)((?:[a-z_-]*token|password|secret|credential|authorization|access[_-]?key)["']?\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`,
)

// Clean bounds local kubeconfig display metadata. Provider messages are never projected.
func Clean(value string) string {
	value = bearerText.ReplaceAllString(value, "Bearer [REDACTED]")
	value = sensitiveText.ReplaceAllString(value, "${1}[REDACTED]")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	if len(value) > 4096 {
		value = strings.ToValidUTF8(value[:4096], "") + " [truncated]"
	}
	return value
}
