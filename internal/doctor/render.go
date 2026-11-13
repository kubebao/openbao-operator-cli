package doctor

import (
	"fmt"
	"io"
	"strings"
)

func WriteText(writer io.Writer, report Report, resultCode string, verbose bool) error {
	var buffer strings.Builder
	target := report.Evidence.Target
	fmt.Fprintf(
		&buffer,
		"Diagnosis: %s / %s/%s\nResult: %s\n",
		target.Context,
		target.Namespace,
		target.Name,
		resultCode,
	)
	if report.ValidationPath != "" {
		fmt.Fprintf(&buffer, "Validation field: %s\n", report.ValidationPath)
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(&buffer, "\n[%s] %s: %s\n  %s\n", finding.Severity, finding.RuleID, finding.Summary, finding.Description)
		for _, check := range finding.SuggestedChecks {
			fmt.Fprintf(&buffer, "  Check: %s\n", check)
		}
		if verbose {
			for _, ref := range finding.Evidence {
				fmt.Fprintf(&buffer, "  Evidence: %s %s/%s UID=%s %s\n", ref.Kind, ref.Namespace, ref.Name, ref.UID, ref.Field)
			}
		}
	}
	for _, coverage := range report.Coverage {
		if verbose {
			fmt.Fprintf(&buffer, "\nRule %s: %s\n", coverage.RuleID, coverage.State)
		}
		for _, missing := range coverage.MissingEvidence {
			fmt.Fprintf(&buffer, "\nMissing evidence (%s): %s\n", coverage.RuleID, missing)
		}
	}
	if len(report.Findings) == 0 {
		buffer.WriteString("\nNo findings in the captured evidence. Inspect rule coverage and collection limits.\n")
	}
	for _, step := range report.Evidence.Collection {
		if verbose || (step.Outcome != "collected" && step.Outcome != "not_applicable" && step.Outcome != "not_found") {
			fmt.Fprintf(&buffer, "Collection: %s %s %s: %s\n", step.APIGroup, step.Resource, step.Action, step.Outcome)
		}
	}
	if verbose && report.Evidence.Related != nil {
		for _, omission := range report.Evidence.Related.Omissions {
			ref := omission.Reference
			fmt.Fprintf(&buffer, "Unverified candidate: %s %s/%s UID=%s: %s\n",
				ref.Kind, ref.Namespace, ref.Name, ref.UID, omission.Reason)
		}
	}
	buffer.WriteString(
		"\nDiagnosis describes captured evidence, not independent OpenBao health probes.\n",
	)
	if _, err := io.WriteString(writer, buffer.String()); err != nil {
		return fmt.Errorf("write diagnosis: %w", err)
	}
	return nil
}
