package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestOutputStripsStylesUnlessColorIsSelected(t *testing.T) {
	t.Parallel()
	text := Status(Bad, "Failed") + " " + Paint(Warn, "pending") + "\n"
	for _, tc := range []struct {
		name    string
		mode    ColorMode
		environ []string
		styled  bool
	}{
		{name: "pipe", mode: ColorAuto},
		{name: "never", mode: ColorNever, environ: []string{"CLICOLOR_FORCE=1"}},
		{name: "always", mode: ColorAlways, styled: true},
		{name: "forced by environment", mode: ColorAuto, environ: []string{"CLICOLOR_FORCE=1", "TERM=xterm"}, styled: true},
		{name: "no color wins over always detection", mode: ColorNever, environ: []string{"NO_COLOR=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buffer bytes.Buffer
			output := NewOutput(&buffer, tc.mode, tc.environ)
			if _, err := output.WriteString(text); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(buffer.String(), "\x1b["); got != tc.styled || output.Styled() != tc.styled {
				t.Fatalf("styled=%v Styled()=%v, want %v: %q", got, output.Styled(), tc.styled, buffer.String())
			}
			if !strings.Contains(buffer.String(), "✗ Failed") && !tc.styled {
				t.Fatalf("plain text lost content: %q", buffer.String())
			}
			if output.Terminal() || output.Width() != defaultWidth {
				t.Fatal("a buffer is not a terminal")
			}
		})
	}
}

func TestNonTerminalOutputWritesNoControlSequences(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	output := NewOutput(&buffer, ColorAlways, nil)
	progress := NewProgress(output)
	progress.Update("waiting")
	progress.Clear()
	if err := output.ClearScreen(); err != nil || buffer.Len() != 0 {
		t.Fatalf("control sequences reached a non-terminal: %q", buffer.String())
	}
}

func TestParseColorMode(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"auto", "always", "never"} {
		if _, err := ParseColorMode(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ParseColorMode("yes"); err == nil {
		t.Fatal("accepted an unsupported mode")
	}
}

func TestLayoutAlignsStyledText(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	text := Fields([]Field{{Label: "Voters", Value: Paint(Good, "3 of 3 ready")}, {Label: "Maintenance", Value: "off"}}, "  ") +
		Table([]string{"RULE", "RESULT"}, [][]string{{Paint(Bad, "KBAO-POD-001"), "evaluated"}, {"KBAO-X", "excluded"}}, "  ") +
		Tree(Node{Label: "OpenBaoCluster prod", Children: []Node{
			{Label: "StatefulSet prod", Children: []Node{{Label: "Pod prod-0"}, {Label: "Pod prod-1"}}},
			{Label: "OpenBaoRestore nightly"},
		}}, "")
	if _, err := Plain(&buffer).WriteString(text); err != nil {
		t.Fatal(err)
	}
	want := `  Voters       3 of 3 ready
  Maintenance  off
  RULE          RESULT
  KBAO-POD-001  evaluated
  KBAO-X        excluded
OpenBaoCluster prod
├── StatefulSet prod
│   ├── Pod prod-0
│   ╰── Pod prod-1
╰── OpenBaoRestore nightly
`
	if buffer.String() != want {
		t.Fatalf("layout mismatch:\n%s\nwant:\n%s", buffer.String(), want)
	}
	if Table([]string{"A"}, nil, "") != "" {
		t.Fatal("an empty table rendered headers")
	}
}

func TestWrapAndTimes(t *testing.T) {
	t.Parallel()
	if got := Wrap("one two three four five six", 34, "    "); got != "    one two three four five six" {
		t.Fatalf("wrap changed short text: %q", got)
	}
	if got := Wrap(strings.Repeat("word ", 12), 30, "  "); strings.Count(got, "\n") < 2 || !strings.HasPrefix(got, "  word") {
		t.Fatalf("wrap did not indent each line: %q", got)
	}
	reference := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for value, want := range map[string]string{
		"2026-11-14T11:59:15Z": "45s ago",
		"2026-11-14T09:30:00Z": "2h 30m ago",
		"2026-11-12T08:00:00Z": "2d 4h ago",
		"2026-11-15T00:00:00Z": "",
		"yesterday":            "",
	} {
		if got := Since(value, reference); got != want {
			t.Fatalf("Since(%q) = %q, want %q", value, got, want)
		}
	}
	if Timestamp("", reference) != "not reported" {
		t.Fatal("missing timestamp is not reported")
	}
}

func TestVocabularyKeepsUnknownCodesReadable(t *testing.T) {
	t.Parallel()
	if TargetResult("brand_new_code") != "returned brand new code" || CollectionOutcome("odd_case") != "odd case" {
		t.Fatal("unknown codes must remain visible")
	}
	if Resource("", "pods") != "pods" || Resource("apps", "statefulsets") != "statefulsets.apps" {
		t.Fatal("resource names")
	}
}

func TestTerminalControlSurvivesDisabledColor(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	output := NewOutput(&buffer, ColorNever, nil)
	output.terminal = true
	progress := NewProgress(output)
	progress.Update("waiting")
	progress.Clear()
	if err := output.ClearScreen(); err != nil {
		t.Fatal(err)
	}
	want := "\r\x1b[2Kwaiting\r\x1b[2K\x1b[H\x1b[2J"
	if buffer.String() != want {
		t.Fatalf("terminal control = %q, want %q", buffer.String(), want)
	}
	if _, err := output.Unstyled().Write([]byte(`{"a":1}`)); err != nil || !strings.HasSuffix(buffer.String(), `{"a":1}`) {
		t.Fatal("unstyled writes must reach the destination unchanged")
	}
}
