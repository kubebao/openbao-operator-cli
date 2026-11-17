package collect

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/render"
)

func TestConnectionRetainsMergedKubeconfigPrecedenceAndExactContext(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "contexts"), filepath.Join(dir, "cluster with ' quote")
	for path, data := range map[string]string{
		first: `apiVersion: v1
kind: Config
current-context: "prod ' context"
contexts:
- name: "prod ' context"
  context: {cluster: original, namespace: security}
`,
		second: `apiVersion: v1
kind: Config
clusters:
- name: original
  cluster: {server: "https://127.0.0.1:1"}
`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KUBECONFIG", strings.Join([]string{first, second}, string(os.PathListSeparator)))
	client, err := NewClient(Options{Name: "bao", APIGroup: "kubebao.org", RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	target := client.Target()
	if target.Namespace != "security" || target.Selection.Context != "prod ' context" ||
		!reflect.DeepEqual(target.Selection.KubeconfigFiles, []string{first, second}) {
		t.Fatalf("connection selection changed: %+v %+v", target, target.Selection)
	}
	command := render.ClusterCommand(target, "status", target.Name)
	if !strings.HasPrefix(command, "env 'KUBECONFIG=") || !strings.Contains(command, "--api-group kubebao.org") ||
		!strings.Contains(command, "-n security") || strings.Contains(command, "--kubeconfig") {
		t.Fatalf("merged selection lost: %s", command)
	}
	// An explicit path overrides the environment's loading precedence.
	client, err = NewClient(Options{Kubeconfig: first, Name: "bao", RequestTimeout: time.Second})
	if err == nil {
		t.Fatalf("explicit incomplete kubeconfig unexpectedly merged the environment: %+v", client.Target())
	}
}
