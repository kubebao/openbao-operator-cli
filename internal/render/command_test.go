package render

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

func TestLocalCommandPreservesArgumentsWithoutShellEvaluation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	program := filepath.Join(dir, "argument printer")
	//nolint:gosec // The test-owned argument printer must be executable.
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s\\000' \"$KUBECONFIG\" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, files := range [][]string{{"/tmp/config with ' quotes"}, {"/tmp/first file", "/tmp/second'file"}} {
		target := evidence.Target{Selection: &evidence.Selection{
			Context: "prod ' $(echo evaluated) `echo evaluated`; *", KubeconfigFiles: files,
		}}
		args := []string{"status", "bao", "-n", "security", "--api-group", "kubebao.org"}
		command := LocalCommand(target, program, args...)
		//nolint:gosec // Exercise quoting with test-controlled arguments and a private argument printer.
		process := exec.Command("/bin/sh", "-c", command)
		process.Env = append(os.Environ(), "KUBECONFIG=unrelated")
		output, err := process.Output()
		if err != nil {
			t.Fatalf("execute argument printer: %v: %s", err, command)
		}
		want := append([]string{"unrelated"}, args...)
		want = append(want, "--context", target.Selection.Context)
		if len(files) == 1 {
			want = append(want, "--kubeconfig", files[0])
		} else {
			want[0] = strings.Join(files, string(os.PathListSeparator))
		}
		if got := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00"); !reflect.DeepEqual(got, want) {
			t.Fatalf("shell changed arguments: got %q want %q", got, want)
		}
	}
}

func TestLocalSelectionIsExcludedFromJSONAndUnsafeCommands(t *testing.T) {
	t.Parallel()
	target := evidence.Target{Context: "display", Selection: &evidence.Selection{
		Context: "exact", KubeconfigFiles: []string{"/private/config"},
	}}
	data, err := json.Marshal(target)
	if err != nil || strings.Contains(string(data), "private") || strings.Contains(string(data), "exact") {
		t.Fatalf("local selection leaked: %s; %v", data, err)
	}
	for _, context := range []string{"secret=canary", "line\ncontext", strings.Repeat("x", 4097), string([]byte{0xff})} {
		target.Selection.Context = context
		if got := ClusterCommand(target, "status", "bao"); got != "" {
			t.Fatalf("lossy selection produced command: %q", got)
		}
	}
	target.Selection = nil
	if got := ClusterCommand(target, "status", "bao"); got != "" {
		t.Fatalf("offline target produced live command: %q", got)
	}
}
