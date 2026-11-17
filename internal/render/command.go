package render

import (
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

// LocalCommand builds a POSIX shell command from exact process-local connection arguments.
// Offline or sanitized display metadata cannot select a live connection.
func LocalCommand(target evidence.Target, executable string, args ...string) string {
	s := target.Selection
	if s == nil || s.Context == "" || len(s.KubeconfigFiles) == 0 {
		return ""
	}
	parts := append([]string{executable}, args...)
	parts = append(parts, "--context", s.Context)
	if len(s.KubeconfigFiles) == 1 {
		parts = append(parts, "--kubeconfig", s.KubeconfigFiles[0])
	} else {
		parts = append([]string{"env", "KUBECONFIG=" + strings.Join(s.KubeconfigFiles, string(os.PathListSeparator))}, parts...)
	}
	for i, value := range parts {
		if !commandValue(value) {
			return ""
		}
		parts[i] = shellQuote(value)
	}
	return strings.Join(parts, " ")
}

// ClusterCommand retains the selected namespace and API group, including for list.
func ClusterCommand(target evidence.Target, verb string, args ...string) string {
	parts := append([]string{verb}, args...)
	parts = append(parts, "-n", target.Namespace)
	if target.APIGroup != "" {
		parts = append(parts, "--api-group", target.APIGroup)
	}
	return LocalCommand(target, "kubebao", parts...)
}

func commandValue(value string) bool {
	return utf8.ValidString(value) && evidence.Clean(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsFunc(value, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("_./:=@%+,-", r)
	}) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
