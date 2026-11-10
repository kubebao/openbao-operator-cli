// Package doctor interprets captured evidence without network access.
package doctor

import (
	"errors"
	"sort"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

const RuleSetVersion = "1"

type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
	None    Severity = "none"
)

const (
	evaluated     = "evaluated"
	insufficient  = "insufficient_evidence"
	notApplicable = "not_applicable"
)

type Finding struct {
	RuleID          string               `json:"ruleID"`
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
	SchemaVersion    string            `json:"schemaVersion"`
	RuleSetVersion   string            `json:"ruleSetVersion"`
	AnalysisTime     time.Time         `json:"analysisTime"`
	FailureThreshold Severity          `json:"failureThreshold"`
	ResultCode       string            `json:"resultCode"`
	ExitCode         int               `json:"exitCode"`
	Findings         []Finding         `json:"findings"`
	Coverage         []RuleCoverage    `json:"coverage"`
	Evidence         evidence.Snapshot `json:"evidence"`
}

func ParseSeverity(value string) (Severity, error) {
	switch Severity(value) {
	case Info, Warning, Error, None:
		return Severity(value), nil
	default:
		return "", errors.New("failure threshold must be info, warning, error, or none")
	}
}

type evaluation struct {
	coverage RuleCoverage
	findings []Finding
}

// Analyze uses the capture's reference time, so equivalent live and stored evidence agree.
func Analyze(snapshot evidence.Snapshot, threshold Severity) Report {
	report := Report{
		SchemaVersion: "v1", RuleSetVersion: RuleSetVersion, AnalysisTime: snapshot.CompletedAt,
		FailureThreshold: threshold, ResultCode: snapshot.ResultCode, ExitCode: snapshot.ExitCode,
		Findings: []Finding{}, Coverage: []RuleCoverage{}, Evidence: snapshot,
	}
	if _, err := ParseSeverity(string(threshold)); err != nil {
		report.ResultCode, report.ExitCode = "invalid_input", 1
		return report
	}
	if snapshot.Observation == nil {
		if report.ExitCode == 0 {
			report.ResultCode, report.ExitCode = "incomplete", 3
		}
		return report
	}
	rules := []func(evidence.Snapshot) evaluation{
		clusterAvailability, workloadReadiness, podReadiness, restrictions, restoreRecovery,
	}
	for _, rule := range rules {
		result := rule(snapshot)
		for i := range result.findings {
			result.findings[i].MissingEvidence = append(result.findings[i].MissingEvidence, result.coverage.MissingEvidence...)
		}
		report.Coverage = append(report.Coverage, result.coverage)
		report.Findings = append(report.Findings, result.findings...)
		if result.coverage.State == insufficient {
			report.ExitCode, report.ResultCode = 3, "incomplete"
		}
	}
	sort.SliceStable(report.Findings, func(i, j int) bool {
		left, right := report.Findings[i], report.Findings[j]
		if left.Severity != right.Severity {
			return severityRank(left.Severity) > severityRank(right.Severity)
		}
		return left.RuleID < right.RuleID
	})
	if report.ExitCode != 0 {
		return report
	}
	for _, finding := range report.Findings {
		if threshold != None && severityRank(finding.Severity) >= severityRank(threshold) {
			report.ExitCode, report.ResultCode = 2, "findings"
			break
		}
	}
	return report
}

func severityRank(severity Severity) int {
	switch severity {
	case Info:
		return 1
	case Warning:
		return 2
	case Error:
		return 3
	default:
		return 4
	}
}

func startRule(id string) evaluation {
	return evaluation{coverage: RuleCoverage{RuleID: id, State: evaluated, MissingEvidence: []string{}}, findings: []Finding{}}
}

func (e *evaluation) missing(message string) {
	e.coverage.State = insufficient
	e.coverage.MissingEvidence = append(e.coverage.MissingEvidence, message)
}

func (e *evaluation) add(severity Severity, summary, description, check string, refs ...evidence.Reference) {
	e.findings = append(e.findings, Finding{
		RuleID: e.coverage.RuleID, Severity: severity, Summary: summary, Description: description,
		Evidence: refs, SuggestedChecks: []string{check}, MissingEvidence: []string{},
	})
}

func clusterReference(snapshot evidence.Snapshot, field string) evidence.Reference {
	ref := evidence.Reference{
		APIVersion: snapshot.Target.APIVersion, Kind: "OpenBaoCluster", Namespace: snapshot.Target.Namespace,
		Name: snapshot.Target.Name, UID: snapshot.Target.UID, Field: field,
	}
	if snapshot.Observation != nil {
		ref.ResourceVersion, ref.CollectedAt = snapshot.Observation.ResourceVersion, snapshot.Observation.CollectedAt
	}
	return ref
}

func field(ref evidence.Reference, name string) evidence.Reference { ref.Field = name; return ref }

func scopeOutcome(snapshot evidence.Snapshot, resource string) string {
	for _, step := range snapshot.Collection {
		if step.Action == "list" && step.Resource == resource {
			return step.Outcome
		}
	}
	return "not_collected"
}
