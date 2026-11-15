package doctor

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
	"github.com/kubebao/openbao-operator-cli/internal/render"
)

// TextOptions select optional sections of the text report.
type TextOptions struct {
	Verbose bool
	// Offline marks a report built from a support bundle; times refer to the capture.
	Offline bool
	Width   int
}

// WriteText writes a human-readable diagnosis. Styling reaches the destination only through a render.Output.
func WriteText(writer io.Writer, report Report, resultCode string, options TextOptions) error {
	if options.Width <= 0 {
		options.Width = 100
	}
	var b strings.Builder
	target := report.Evidence.Target
	b.WriteString(heading(target, options.Offline) + "\n")
	if options.Offline {
		b.WriteString(render.Faint(fmt.Sprintf("Captured %s to %s · analysed as of %s",
			report.Evidence.StartedAt.Format(time.RFC3339), report.Evidence.CompletedAt.Format(time.RFC3339),
			report.AnalysisTime.Format(time.RFC3339))) + "\n")
	}
	if report.Evidence.NormalizedEvidence == nil {
		writeUnavailable(&b, report, target, options.Width)
	} else {
		writeSummary(&b, report, resultCode)
		writeFindings(&b, report, target, options)
		writeGaps(&b, report)
	}
	if tree := ownershipTree(report.Evidence, options.Verbose); tree != "" {
		b.WriteString("\n" + render.Heading("Resources") + "\n" + tree)
	}
	writeCollection(&b, report.Evidence.Collection, options.Verbose)
	if options.Verbose {
		writeCoverage(&b, report.Coverage)
	}
	b.WriteString("\n" + render.Faint("Findings describe collected Kubernetes evidence, not live OpenBao health probes.") + "\n")
	if _, err := io.WriteString(writer, b.String()); err != nil {
		return fmt.Errorf("write diagnosis: %w", err)
	}
	return nil
}

func heading(target evidence.Target, offline bool) string {
	title := "Diagnosis of " + render.Bold(target.Name)
	if offline {
		title = "Offline diagnosis of " + render.Bold(target.Name)
	}
	context := []string{"namespace " + target.Namespace}
	if target.Context != "" {
		context = append(context, "context "+target.Context)
	}
	return title + "  " + render.Faint(strings.Join(context, " · "))
}

func writeUnavailable(b *strings.Builder, report Report, target evidence.Target, width int) {
	if report.ValidationPath != "" {
		b.WriteString("\n" + render.Status(render.Bad, "Evidence rejected") + "\n")
		b.WriteString(render.Wrap("The field "+report.ValidationPath+" cannot be interpreted, so no rules ran.", width, "  ") + "\n")
		return
	}
	b.WriteString("\n" + render.Status(render.Bad, "Diagnosis not possible") + "\n")
	b.WriteString("  OpenBaoCluster " + target.Name + " " + render.TargetResult(report.Evidence.ResultCode) + ".\n")
	if report.Evidence.ResultCode == "not_found" {
		b.WriteString(render.NextCommand("Check the namespace and context, or list clusters:", "kubectl get openbaoclusters -A", "  "))
	}
}

func writeSummary(b *strings.Builder, report Report, resultCode string) {
	counts := map[Severity]int{}
	for _, f := range report.Findings {
		counts[f.Severity]++
	}
	parts := []string{}
	for _, s := range []Severity{Error, Warning, Info} {
		if counts[s] > 0 {
			parts = append(parts, plural(counts[s], map[Severity]string{Error: "error", Warning: "warning", Info: "note"}[s]))
		}
	}
	gaps := 0
	for _, c := range report.Coverage {
		if c.State == "insufficient_evidence" {
			gaps++
		}
	}
	incomplete := resultCode == "incomplete" || gaps > 0 || !report.Complete
	tone, label := render.Good, "No issues found"
	switch {
	case counts[Error] > 0:
		tone, label = render.Bad, strings.Join(parts, ", ")
	case counts[Warning] > 0:
		tone, label = render.Warn, strings.Join(parts, ", ")
	case counts[Info] > 0:
		label = "No problems found; " + strings.Join(parts, ", ")
	case incomplete:
		tone, label = render.Unknown, "No issues found in the evidence that was collected"
	}
	b.WriteString("\n" + render.Status(tone, label) + "\n")
	if incomplete {
		detail := "Diagnosis is incomplete"
		if gaps > 0 {
			detail += ": " + plural(gaps, "rule") + " could not be evaluated"
		}
		b.WriteString(render.Status(render.Unknown, detail) + "\n")
	}
	b.WriteString(render.Faint(coverageSummary(report.Coverage)) + "\n")
}

func coverageSummary(coverage []RuleCoverage) string {
	counts := map[string]int{}
	for _, c := range coverage {
		counts[c.State]++
	}
	parts := []string{}
	for _, state := range []string{"evaluated", "insufficient_evidence", "not_applicable", "excluded", "not_requested"} {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], CoverageState(state)))
		}
	}
	return "Rules: " + strings.Join(parts, " · ")
}

func writeFindings(b *strings.Builder, report Report, target evidence.Target, options TextOptions) {
	findings := slices.Clone(report.Findings)
	slices.SortStableFunc(findings, func(a, b Finding) int { return cmp.Compare(severityRank(b.Severity), severityRank(a.Severity)) })
	for _, f := range findings {
		tone := severityTone(f.Severity)
		subject := strings.Replace(f.Summary, " "+target.Namespace+"/", " ", 1)
		if f.RuleID == "KBAO-CONDITION-001" && f.Subsubject != "" {
			subject = f.Subsubject
		}
		b.WriteString("\n" + render.Status(tone, strings.ToUpper(string(f.Severity))) + "  " +
			render.Bold(RuleTitle(f.RuleID)+": "+subject) + "  " + render.Faint(f.RuleID) + "\n")
		b.WriteString(render.Wrap(f.Description, options.Width, "  ") + "\n")
		for _, check := range f.SuggestedChecks {
			b.WriteString(render.Wrap(check, options.Width, "  ") + "\n")
		}
		if command := kubectlCommand(f.Evidence, target); command != "" {
			label := "Inspect it:"
			if strings.Contains(command, "jsonpath=") {
				label = "Show the condition with the operator's message:"
			}
			b.WriteString(render.NextCommand(label, command, "  "))
		}
		if options.Verbose {
			for _, ref := range f.Evidence {
				b.WriteString(render.Faint(fmt.Sprintf("  evidence: %s %s/%s · %s · uid %s", ref.Kind, ref.Namespace, ref.Name,
					orNone(ref.Field), ref.UID)) + "\n")
			}
		}
	}
}

// kubectlCommand suggests a read-only command for the first referenced resource. An OpenBaoCluster condition reference
// prints only that condition, including the operator's message, which kubebao does not project. Other resources use
// describe, which also shows their events.
func kubectlCommand(refs []evidence.Reference, target evidence.Target) string {
	if len(refs) == 0 || refs[0].Name == "" {
		return ""
	}
	ref := refs[0]
	resource := strings.ToLower(ref.Kind)
	if group, _, found := strings.Cut(ref.APIVersion, "/"); found && strings.Contains(group, ".") && group != "apps" {
		resource = strings.ToLower(ref.Kind) + "s." + group
	}
	parts := []string{"kubectl"}
	if target.Context != "" {
		parts = append(parts, "--context", target.Context)
	}
	if ref.Namespace != "" {
		parts = append(parts, "-n", ref.Namespace)
	}
	for _, candidate := range refs {
		condition, found := strings.CutPrefix(candidate.Field, "status.conditions[type=")
		if found && strings.HasSuffix(condition, "]") && candidate.Name == ref.Name && ref.Kind == "OpenBaoCluster" {
			jsonpath := `'{.status.conditions[?(@.type=="` + strings.TrimSuffix(condition, "]") + `")]}'`
			return strings.Join(append(parts, "get", resource, ref.Name, "-o", "jsonpath="+jsonpath), " ")
		}
	}
	return strings.Join(append(parts, "describe", resource, ref.Name), " ")
}

func writeGaps(b *strings.Builder, report Report) {
	rows := [][]string{}
	for _, c := range report.Coverage {
		if c.State == "insufficient_evidence" {
			rows = append(rows, []string{
				render.Symbol(render.Unknown) + " " + RuleTitle(c.RuleID), gapReason(report.Evidence, c),
				render.Faint(c.RuleID),
			})
		}
	}
	if len(rows) > 0 {
		b.WriteString("\n" + render.Heading("Evidence gaps") + "\n")
		b.WriteString(render.Table([]string{"CHECK", "WHY IT COULD NOT BE EVALUATED", "RULE"}, rows, "  "))
	}
}

func writeCollection(b *strings.Builder, steps []evidence.Step, verbose bool) {
	table := render.CollectionTable(steps, verbose, "  ")
	if table == "" {
		return
	}
	title := "Collection problems"
	if verbose {
		title = "Collection"
	}
	b.WriteString("\n" + render.Heading(title) + "\n" + table)
}

func writeCoverage(b *strings.Builder, coverage []RuleCoverage) {
	rows := [][]string{}
	for _, c := range coverage {
		tone := render.Good
		switch c.State {
		case "insufficient_evidence":
			tone = render.Unknown
		case "not_applicable", "excluded", "not_requested":
			tone = render.Neutral
		}
		rows = append(
			rows,
			[]string{c.RuleID, RuleTitle(c.RuleID), render.Paint(tone, CoverageState(c.State)), MissingReasons(c.MissingEvidence)},
		)
	}
	if len(rows) > 0 {
		b.WriteString("\n" + render.Heading("Rule coverage") + "\n")
		b.WriteString(render.Table([]string{"RULE", "CHECK", "RESULT", "MISSING EVIDENCE"}, rows, "  "))
	}
}

func severityRank(s Severity) int {
	switch s {
	case Error:
		return 3
	case Warning:
		return 2
	case Info:
		return 1
	default:
		return 0
	}
}

func severityTone(s Severity) render.Tone {
	switch s {
	case Error:
		return render.Bad
	case Warning:
		return render.Warn
	default:
		return render.Info
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func orNone(value string) string {
	if value == "" {
		return "no field"
	}
	return value
}

// gapReason explains insufficient coverage. Condition gaps name the conditions that blocked evaluation.
func gapReason(snapshot evidence.Snapshot, coverage RuleCoverage) string {
	if gaps := ConditionGaps(snapshot); coverage.RuleID == "KBAO-CONDITION-001" && len(gaps) > 0 {
		return strings.Join(gaps, "; ")
	}
	return MissingReasons(coverage.MissingEvidence)
}
