package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/kubebao/openbao-operator-cli/internal/evidence"
)

// TargetResult explains a collection result code for the selected cluster. Codes stay in JSON;
// text output uses these phrases. Unknown codes fall back to a readable form of the code.
func TargetResult(code string) string {
	switch code {
	case "completed":
		return "was read completely"
	case "not_found":
		return "was not found"
	case "ambiguous_api_group":
		return "is served by both kubebao.org and openbao.org; choose one with --api-group"
	case "incomplete":
		return "could not be read completely"
	case "resource_deleted":
		return "was deleted during collection"
	case "resource_replaced":
		return "was replaced by a resource with a different UID"
	case "target_changed":
		return "changed during collection"
	case "target_verification_incomplete":
		return "could not be verified again after collection"
	case "invalid_resource":
		return "reports fields kubebao cannot interpret"
	case "invalid_input":
		return "could not be selected because the input is invalid"
	default:
		return "returned " + Humanize(code)
	}
}

// CollectionOutcome explains the outcome of one read.
func CollectionOutcome(outcome string) string {
	switch outcome {
	case "collected", "complete":
		return "read"
	case "denied":
		return "permission denied"
	case "not_found":
		return "not found"
	case "not_applicable":
		return "API not served"
	case "limit_reached":
		return "stopped at a size or page limit"
	case "repeated_continue_token":
		return "stopped: the API server repeated a page"
	case "timeout":
		return "timed out"
	case "cancelled":
		return "cancelled"
	case "rate_limited":
		return "rate limited by the API server"
	case "expired":
		return "list expired before it finished"
	case "partial":
		return "partially read"
	case "invalid":
		return "response could not be interpreted"
	case "failed":
		return "request failed"
	default:
		return Humanize(outcome)
	}
}

// CollectionTone classifies a read outcome.
func CollectionTone(outcome string) Tone {
	switch outcome {
	case "collected", "complete", "not_applicable", "not_found":
		return Good
	case "denied", "invalid", "failed":
		return Bad
	default:
		return Warn
	}
}

// CollectionProblem reports whether a read outcome limits the evidence.
func CollectionProblem(outcome string) bool {
	return outcome != "collected" && outcome != "not_applicable" && outcome != "not_found"
}

// CollectionTable renders read steps. With all false, only problem steps appear.
func CollectionTable(steps []evidence.Step, all bool, indent string) string {
	rows := [][]string{}
	for _, step := range steps {
		if !all && !CollectionProblem(step.Outcome) {
			continue
		}
		rows = append(rows, []string{
			Resource(step.APIGroup, step.Resource),
			Humanize(step.Action),
			Symbol(CollectionTone(step.Outcome)) + " " + CollectionOutcome(step.Outcome),
		})
	}
	return Table([]string{"RESOURCE", "ACTION", "RESULT"}, rows, indent)
}

// Resource names a resource type with its API group, using "core" for the empty group.
func Resource(group, resource string) string {
	switch {
	case resource == "" && group == "":
		return "core"
	case resource == "":
		return group
	case group == "":
		return resource
	default:
		return resource + "." + group
	}
}

// Humanize turns a snake_case code into words.
func Humanize(code string) string {
	return strings.ReplaceAll(code, "_", " ")
}

// Since describes the age of an RFC 3339 timestamp relative to reference, such as "3h ago".
// Unparseable or future values return an empty string.
func Since(value string, reference time.Time) string {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil || reference.IsZero() || at.After(reference) {
		return ""
	}
	return Duration(reference.Sub(at)) + " ago"
}

// Duration formats a duration with at most two units, such as "2d 4h" or "45s".
func Duration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	switch {
	case days > 0 && hours > 0:
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h"
	case days > 0:
		return strconv.Itoa(days) + "d"
	case hours > 0 && minutes > 0:
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	case hours > 0:
		return strconv.Itoa(hours) + "h"
	case minutes > 0 && seconds > 0:
		return strconv.Itoa(minutes) + "m " + strconv.Itoa(seconds) + "s"
	case minutes > 0:
		return strconv.Itoa(minutes) + "m"
	default:
		return strconv.Itoa(seconds) + "s"
	}
}

// Timestamp shows an RFC 3339 value with its age relative to reference when known.
func Timestamp(value string, reference time.Time) string {
	if value == "" {
		return "not reported"
	}
	if age := Since(value, reference); age != "" {
		return value + " " + Faint("("+age+")")
	}
	return value
}
