package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/doctor"
	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/status"
	"github.com/kubebao/openbao-operator-client/diagnose"
	shared "github.com/kubebao/openbao-operator-client/evidence"
)

// Process results belong to command envelopes, not normalized evidence or pure analysis.
type statusReport struct {
	evidence.Snapshot
	ExitCode       int    `json:"exitCode"`
	ValidationPath string `json:"validationPath,omitempty"`
}
type diagnosticReport struct {
	doctor.Report
	Source           string       `json:"source"`
	FailureThreshold string       `json:"failureThreshold"`
	ResultCode       string       `json:"resultCode"`
	ExitCode         int          `json:"exitCode"`
	Evidence         statusReport `json:"evidence"`
}

func collectionExitCode(code string) int {
	switch code {
	case "completed":
		return 0
	case "not_found", "ambiguous_api_group", "invalid_input":
		return 1
	default:
		return 3
	}
}

func parseFailureThreshold(value string) error {
	switch value {
	case "none", "info", "warning", "error":
		return nil
	default:
		return errors.New("failure threshold must be info, warning, error, or none")
	}
}

func findingFails(severity doctor.Severity, threshold string) bool {
	rank := map[string]int{"info": 1, "warning": 2, "error": 3}
	return threshold != "none" && rank[string(severity)] >= rank[threshold]
}

func diagnosticResultWithPolicy(snapshot evidence.Snapshot, threshold string, policy diagnose.Policy) diagnosticReport {
	analyzed, err := doctor.Analyze(snapshot, policy)
	report := diagnosticReport{
		Report:           analyzed,
		Source:           "live",
		FailureThreshold: threshold,
		ResultCode:       snapshot.ResultCode,
		ExitCode: collectionExitCode(
			snapshot.ResultCode,
		),
		Evidence: statusReport{Snapshot: analyzed.Evidence, ExitCode: collectionExitCode(snapshot.ResultCode)},
	}
	if report.ExitCode != 0 {
		return report
	}
	if err != nil || !analyzed.Complete {
		report.ResultCode, report.ExitCode = "incomplete", 3
		return report
	}
	for _, finding := range report.Findings {
		if findingFails(finding.Severity, threshold) {
			report.ResultCode, report.ExitCode = "findings", 2
			break
		}
	}
	return report
}

func diagnosticPolicy(snapshot evidence.Snapshot, maxBackupAge time.Duration) diagnose.Policy {
	return doctor.WithBackupAge(doctor.Policy(snapshot.CompletedAt), maxBackupAge)
}

func writeJSON(writer io.Writer, value any) error {
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return fmt.Errorf("write command JSON: %w", err)
	}
	return nil
}

func writeStatus(writer io.Writer, report statusReport, output string, verbose bool) error {
	if output == "json" {
		return writeJSON(writer, report)
	}
	if err := status.WriteText(writer, report.Snapshot, verbose, report.ExitCode == 3); err != nil {
		return err
	}
	if report.ValidationPath != "" {
		if _, err := fmt.Fprintf(writer, "Validation field: %s\n", report.ValidationPath); err != nil {
			return fmt.Errorf("write validation field: %w", err)
		}
	}
	return nil
}

func writeDiagnostic(writer io.Writer, report diagnosticReport, output string, verbose bool) error {
	if output == "json" {
		return writeJSON(writer, report)
	}
	if report.Source == "bundle" {
		if _, err := fmt.Fprintf(writer, "Offline analysis: captured %s to %s; reference time %s\n",
			report.Evidence.StartedAt.Format(time.RFC3339Nano), report.Evidence.CompletedAt.Format(time.RFC3339Nano),
			report.AnalysisTime.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("write offline analysis times: %w", err)
		}
	}
	return doctor.WriteText(writer, report.Report, report.ResultCode, verbose)
}

func makeStatusReport(snapshot evidence.Snapshot) statusReport {
	path := ""
	if snapshot.Observation != nil {
		attention, err := doctor.StatusAttention(snapshot)
		if err != nil {
			snapshot.ResultCode = "invalid_resource"
			snapshot.NormalizedEvidence = nil
			var invalid *shared.ValidationError
			if errors.As(err, &invalid) {
				path = invalid.Path
			}
		} else {
			snapshot.Observation.Attention = attention
		}
	}
	return statusReport{Snapshot: snapshot, ExitCode: collectionExitCode(snapshot.ResultCode), ValidationPath: path}
}
