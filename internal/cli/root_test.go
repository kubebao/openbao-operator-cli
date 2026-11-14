package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-cli/internal/buildinfo"
)

func TestCommandOutputAndExitCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"root help", nil, 0},
		{"help flag", []string{"--help"}, 0},
		{"version flag", []string{"--version"}, 0},
		{"version command", []string{"version"}, 0},
		{"unknown command", []string{"unknown-command"}, 1},
		{"unknown flag", []string{"version", "--unknown"}, 1},
		{"extra argument", []string{"version", "prod"}, 1},
		{"invalid output", []string{"version", "--output", "yaml"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := Execute(context.Background(), tc.args, &stdout, &stderr); code != tc.code {
				t.Fatalf("exit code = %d, want %d; stderr: %s", code, tc.code, &stderr)
			}
			if tc.code == 0 {
				if stdout.Len() == 0 || stderr.Len() != 0 {
					t.Fatalf("successful command: stdout = %q, stderr = %q", &stdout, &stderr)
				}
			} else if stdout.Len() != 0 || strings.Count(stderr.String(), "Error:") != 1 {
				t.Fatalf("failed command: stdout = %q, stderr = %q", &stdout, &stderr)
			}
		})
	}
}

func TestVersionJSONContract(t *testing.T) {
	t.Parallel()
	info := buildinfo.Info{Version: "v0.1.0", Commit: "0123abcd", GoVersion: "go1.27.3", OS: "linux", Arch: "arm64"}
	command := newCommand(info)
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"version", "--output", "json"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v; output: %s", err, &stdout)
	}
	want := map[string]string{
		"schemaVersion": "v1", "version": info.Version, "commit": info.Commit,
		"goVersion": info.GoVersion, "os": info.OS, "arch": info.Arch,
	}
	if len(result) != len(want) || stderr.Len() != 0 {
		t.Fatalf("unexpected version output: %v; stderr: %s", result, &stderr)
	}
	for key, value := range want {
		if result[key] != value {
			t.Errorf("%s = %q, want %q", key, result[key], value)
		}
	}
}

func TestPluginInvocation(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"status", "--help"}, {"doctor", "--help"}, {"restart", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := execute(context.Background(), "/plugins/kubectl-kubebao", args, &stdout, &stderr)
			if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "kubectl kubebao") {
				t.Fatalf("exit code = %d; stdout = %q; stderr = %q", code, &stdout, &stderr)
			}
		})
	}
	var standalone, plugin, stderr bytes.Buffer
	args := []string{"version", "--output", "json"}
	if execute(context.Background(), "kubebao", args, &standalone, &stderr) != 0 ||
		execute(context.Background(), "kubectl-kubebao", args, &plugin, &stderr) != 0 ||
		standalone.String() != plugin.String() || stderr.Len() != 0 {
		t.Fatalf("standalone = %q; plugin = %q; stderr = %q", &standalone, &plugin, &stderr)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

func TestVersionOutputFailure(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			code := Execute(context.Background(), []string{"version", "--output", format}, failingWriter{}, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), "write version: output unavailable") {
				t.Fatalf("exit code = %d; stderr: %s", code, &stderr)
			}
		})
	}
}
