package doctor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kubebao/openbao-operator-client/diagnose"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

func TestConditionFindingsAreTitledByConditionWithATargetedCommand(t *testing.T) {
	t.Parallel()
	target := evidence.Target{
		Selection: &evidence.Selection{Context: "demo", KubeconfigFiles: []string{"/config"}},
		Context:   "demo",
		Namespace: "team-a",
		Name:      "bao",
		UID:       "uid-1",
	}
	ref := evidence.Reference{APIVersion: "kubebao.org/v1alpha1", Kind: "OpenBaoCluster", Namespace: "team-a", Name: "bao", UID: "uid-1"}
	generation, condition := ref, ref
	generation.Field, condition.Field = "metadata.generation", "status.conditions[type=TLSReady]"
	report := Report{
		Complete: true,
		Evidence: evidence.Snapshot{Target: target, ResultCode: "completed", NormalizedEvidence: &diagnose.Capture{}},
		Findings: []Finding{
			{
				RuleID: "KBAO-CONDITION-001", Subsubject: "EtcdEncryptionWarning", Severity: Info, Summary: "OpenBaoCluster team-a/bao",
				Description: "The operator cannot verify encryption.", SuggestedChecks: []string{conditionCheck("EtcdEncryptionWarning")},
			},
			{
				RuleID: "KBAO-CONDITION-001", Subsubject: "TLSReady", Severity: Warning, Summary: "OpenBaoCluster team-a/bao",
				Description: "The operator reports TLSReady=False for the current generation.",
				Evidence:    []evidence.Reference{generation, condition}, SuggestedChecks: []string{conditionCheck("TLSReady")},
			},
		},
		Coverage: []RuleCoverage{{RuleID: "KBAO-CONDITION-001", State: "evaluated"}},
	}
	var buffer bytes.Buffer
	if err := WriteText(render.Plain(&buffer), report, "findings", TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := buffer.String()
	for _, want := range []string{
		"! 1 warning, 1 note",
		"! WARNING  Operator conditions: TLSReady  KBAO-CONDITION-001",
		"• INFO  Operator conditions: EtcdEncryptionWarning",
		"→ Show the condition with the operator's message:",
		`kubectl -n team-a get openbaoclusters.kubebao.org bao -o ` + `'jsonpath={.status.conditions[?(@.type=="TLSReady")]}'` +
			` --context demo --kubeconfig /config`,
		"encrypts Secrets at rest",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Index(text, "TLSReady  KBAO") > strings.Index(text, "EtcdEncryptionWarning") {
		t.Fatalf("warning is not ordered before the note:\n%s", text)
	}
}
