// Package doctor maps shared analysis into CLI findings without network access.
package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

type Finding struct {
	ID     string `json:"id"`
	RuleID string `json:"ruleID"`
	// Subsubject names the part of the subject a finding concerns, such as a condition type.
	Subsubject      string               `json:"subsubject,omitempty"`
	Severity        Severity             `json:"severity"`
	Summary         string               `json:"summary"`
	Description     string               `json:"description"`
	Evidence        []evidence.Reference `json:"evidence"`
	SuggestedChecks []string             `json:"suggestedChecks"`
	MissingEvidence []string             `json:"missingEvidence"`
}
type RuleCoverage struct {
	RuleID          string   `json:"ruleID"`
	State           string   `json:"state"`
	MissingEvidence []string `json:"missingEvidence"`
}
type Report struct {
	ValidationPath string            `json:"validationPath,omitempty"`
	SchemaVersion  string            `json:"schemaVersion"`
	AnalysisTime   time.Time         `json:"analysisTime"`
	Policy         diagnose.Policy   `json:"policy"`
	Findings       []Finding         `json:"findings"`
	Coverage       []RuleCoverage    `json:"coverage"`
	Evidence       evidence.Snapshot `json:"evidence"`
	Complete       bool              `json:"-"`
}

func Policy(at time.Time) diagnose.Policy {
	p := diagnose.DefaultPolicy(at)
	p.Rules = []diagnose.RuleID{
		diagnose.RuleGeneration,
		diagnose.RuleAvailability,
		diagnose.RuleSealed,
		diagnose.RuleCondition,
		diagnose.RuleController,
		diagnose.RuleBackupFailed,
		diagnose.RuleBackupAbsent,
		diagnose.RuleUpgrade,
		diagnose.RuleWorkload,
		diagnose.RulePod,
		diagnose.RuleRestriction,
		diagnose.RuleRestore,
	}
	p.ReportHistoricalRestores = false
	p.MaxSubjects = 3001
	return p
}

// WithBackupAge selects age evaluation only for a caller-declared positive limit.
func WithBackupAge(p diagnose.Policy, limit time.Duration) diagnose.Policy {
	if limit > 0 {
		p.MaxBackupAge = limit
		p.Rules = append(p.Rules, diagnose.RuleBackupAge)
	}
	return p
}

func StatusPolicy(at time.Time) diagnose.Policy {
	p := Policy(at)
	p.Rules = []diagnose.RuleID{
		diagnose.RuleGeneration,
		diagnose.RuleAvailability,
		diagnose.RuleSealed,
		diagnose.RuleCondition,
		diagnose.RuleController,
		diagnose.RuleRestriction,
		diagnose.RuleBackupFailed,
		diagnose.RuleUpgrade,
	}
	return p
}

func Analyze(snapshot evidence.Snapshot, policy diagnose.Policy) (Report, error) {
	report := Report{
		SchemaVersion: "v1",
		AnalysisTime:  policy.ReferenceTime,
		Policy:        policy,
		Findings:      []Finding{},
		Coverage:      []RuleCoverage{},
		Evidence:      snapshot,
		Complete:      false,
	}
	if snapshot.NormalizedEvidence == nil {
		if snapshot.ResultCode != "completed" {
			return report, nil
		}
		return report, errors.New("normalized evidence is missing")
	}
	result, err := diagnose.Analyze(*snapshot.NormalizedEvidence, policy)
	if err != nil {
		// Rejected captures have not passed validation and must not enter output.
		report.Evidence.NormalizedEvidence = nil
		var invalid *shared.ValidationError
		if errors.As(err, &invalid) {
			report.ValidationPath = invalid.Path
			return report, fmt.Errorf("normalized evidence cannot be evaluated: %w", err)
		}
		return report, errors.New("normalized evidence cannot be evaluated")
	}
	report.Complete = true
	missing := map[diagnose.RuleID][]string{}
	for _, c := range result.Coverage {
		state := string(c.State)
		limits := []string{}
		if c.State == diagnose.Insufficient {
			report.Complete = false
			state = "insufficient_evidence"
			for _, reason := range c.Reasons {
				limits = append(limits, string(reason))
			}
			if len(limits) == 0 {
				limits = append(limits, string(c.Reason))
			}
			missing[c.Rule] = limits
		}
		if c.State == diagnose.NotRequested {
			report.Complete = false
		}
		report.Coverage = append(report.Coverage, RuleCoverage{RuleID: string(c.Rule), State: state, MissingEvidence: limits})
	}
	for _, f := range result.Findings {
		key, keyErr := f.Key()
		if keyErr != nil {
			return report, errors.New("invalid finding identity")
		}
		sum := sha256.Sum256([]byte(key))
		severity := Warning
		switch f.Severity {
		case diagnose.Info:
			severity = Info
		case diagnose.Error, diagnose.Critical:
			severity = Error
		}
		value := Finding{
			ID: string(
				f.Rule,
			) + ":" + hex.EncodeToString(
				sum[:16],
			),
			RuleID:          string(f.Rule),
			Subsubject:      f.Subsubject,
			Severity:        severity,
			Summary:         f.Subject.Kind + " " + f.Subject.Namespace + "/" + f.Subject.Name,
			Description:     f.Summary,
			Evidence:        []evidence.Reference{},
			SuggestedChecks: []string{suggestedCheck(f.Rule, f.Subsubject)},
			MissingEvidence: append([]string{}, missing[f.Rule]...),
		}
		for _, ref := range f.References {
			value.Evidence = append(value.Evidence, evidence.Reference{
				APIVersion:      ref.APIVersion,
				Kind:            ref.Identity.Kind,
				Namespace:       ref.Identity.Namespace,
				Name:            ref.Identity.Name,
				UID:             ref.Identity.UID,
				ResourceVersion: stringValue(ref.ResourceVersion),
				CollectedAt:     *ref.CollectedAt.Value,
				Field:           ref.Field,
			})
		}
		report.Findings = append(report.Findings, value)
	}
	return report, nil
}

func stringValue(value shared.Text) string {
	if value.Value == nil {
		return ""
	}
	return *value.Value
}

func suggestedCheck(rule diagnose.RuleID, subsubject string) string {
	if rule == diagnose.RuleCondition {
		return conditionCheck(subsubject)
	}
	switch rule {
	case diagnose.RuleAvailability:
		return "Inspect current resource and Available condition acknowledgement."
	case diagnose.RuleWorkload, diagnose.RulePod:
		return "Inspect the UID-confirmed workload and its reported readiness."
	case diagnose.RuleRestore:
		return "Inspect the UID-confirmed restore and any matching hold; do not replay an ambiguous submission."
	case diagnose.RuleRestriction:
		return "Inspect independent pause, maintenance, restore-hold, and break-glass controls."
	case diagnose.RuleBackupFailed, diagnose.RuleBackupAbsent, diagnose.RuleBackupAge:
		return "Inspect recorded backup results and the declared backup policy; configuration does not verify provider access."
	case diagnose.RuleUpgrade:
		return "Inspect the recorded upgrade failure and current operator progress before taking action."
	default:
		return "Inspect the referenced public operator fields."
	}
}

func StatusAttention(snapshot evidence.Snapshot) ([]evidence.Attention, error) {
	report, err := Analyze(snapshot, StatusPolicy(snapshot.CompletedAt))
	if err != nil {
		return nil, err
	}
	result := []evidence.Attention{}
	for _, f := range report.Findings {
		result = append(result, evidence.Attention{
			Code: f.RuleID, Severity: string(f.Severity), Summary: f.Description, Field: specificField(f.Evidence),
		})
	}
	for _, c := range report.Coverage {
		if c.State == "insufficient_evidence" {
			result = append(
				result,
				evidence.Attention{Code: c.RuleID + "_incomplete", Summary: c.RuleID + ": current evidence is insufficient for evaluation"},
			)
		}
	}
	return result, nil
}

// conditionCheck suggests the next step for a recognized operator condition.
func conditionCheck(condition string) string {
	switch condition {
	case "EtcdEncryptionWarning":
		return "Confirm that the Kubernetes API server encrypts Secrets at rest with an encryption configuration. " +
			"The operator cannot check this; the advisory remains after encryption is enabled."
	case "Degraded":
		return "Read the condition's reason and message, then inspect recent events for the OpenBaoCluster."
	case "SecurityRisk", "NodeSecurityCapabilityMismatch":
		return "Read the condition's message; it names the security guardrail that is not enforced."
	case "TLSReady", "ACMEIntegrationReady", "ACMECacheReady":
		return "Read the condition's message and inspect the TLS or ACME configuration and its certificate Secrets."
	case "BackupConfigurationReady":
		return "Read the condition's message and inspect the backup target and its credentials reference."
	case "ReadReplicaStorageConfigured", "ReadReplicasReady", "ReadServingAvailable", "ReadReplicasAutopilotHealthy":
		return "Inspect the read-replica StatefulSet, its Pods, and their storage."
	default:
		return "Read the condition's reason and message on the OpenBaoCluster."
	}
}

// specificField prefers the most specific referenced field over generation acknowledgement.
func specificField(refs []evidence.Reference) string {
	field := ""
	for _, ref := range refs {
		if field == "" || field == "metadata.generation" {
			field = ref.Field
		}
	}
	return field
}
