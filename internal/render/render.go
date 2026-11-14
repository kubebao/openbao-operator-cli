// Package render owns terminal presentation for human-readable command output.
//
// Views build styled strings with the helpers in this package and write them
// through an Output. The Output decides whether escape sequences reach the
// destination: it keeps them for colour terminals and strips them for pipes,
// files, NO_COLOR, and --color=never. JSON output never passes through here.
// Only this package imports the styling library.
package render

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// ColorMode selects when styled output is written.
type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// ParseColorMode validates a --color value.
func ParseColorMode(value string) (ColorMode, error) {
	switch mode := ColorMode(value); mode {
	case ColorAuto, ColorAlways, ColorNever:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported color mode %q: use auto, always, or never", value)
	}
}

const (
	defaultWidth = 100
	maxWidth     = 120
	minWidth     = 40
)

// Output writes rendered text to a destination with the detected or selected colour profile.
type Output struct {
	writer   *colorprofile.Writer
	terminal bool
	width    int
}

// NewOutput detects whether destination is a terminal and which colour profile applies.
// Auto mode honours NO_COLOR, CLICOLOR, CLICOLOR_FORCE, and TERM through the environment.
func NewOutput(destination io.Writer, mode ColorMode, environ []string) *Output {
	output := &Output{writer: colorprofile.NewWriter(destination, environ), width: defaultWidth}
	if file, ok := destination.(interface{ Fd() uintptr }); ok && term.IsTerminal(file.Fd()) {
		output.terminal = true
		if width, _, err := term.GetSize(file.Fd()); err == nil && width > 0 {
			output.width = min(max(width, minWidth), maxWidth)
		}
	}
	switch mode {
	case ColorNever:
		output.writer.Profile = colorprofile.NoTTY
	case ColorAlways:
		if output.writer.Profile < colorprofile.ANSI {
			output.writer.Profile = colorprofile.ANSI
		}
	case ColorAuto:
	}
	return output
}

// Plain returns an Output that never writes escape sequences, for files such as bundle summaries.
func Plain(destination io.Writer) *Output {
	return NewOutput(destination, ColorNever, nil)
}

// Write implements io.Writer and downsamples or strips escape sequences for the destination.
func (o *Output) Write(p []byte) (int, error) {
	n, err := o.writer.Write(p)
	if err != nil {
		return n, fmt.Errorf("write output: %w", err)
	}
	return n, nil
}

// WriteString writes rendered text.
func (o *Output) WriteString(s string) (int, error) { return o.Write([]byte(s)) }

// Terminal reports whether the destination is an interactive terminal.
func (o *Output) Terminal() bool { return o.terminal }

// Styled reports whether colour and text attributes reach the destination.
func (o *Output) Styled() bool { return o.writer.Profile >= colorprofile.ANSI }

// Width is the layout width: the terminal width bounded to a readable range, or a fixed default.
func (o *Output) Width() int { return o.width }

// Unstyled returns the destination itself, for machine-readable output such as JSON.
func (o *Output) Unstyled() io.Writer { return o.writer.Forward }

// control writes cursor and erase sequences to a terminal. They bypass the colour profile,
// which strips every escape sequence when colour is disabled.
func (o *Output) control(sequence string) error {
	if !o.terminal {
		return nil
	}
	if _, err := io.WriteString(o.writer.Forward, sequence); err != nil {
		return fmt.Errorf("write terminal control: %w", err)
	}
	return nil
}

// ClearScreen moves the cursor home and clears a terminal. It writes nothing elsewhere.
func (o *Output) ClearScreen() error {
	return o.control(ansi.CursorHomePosition + ansi.EraseEntireScreen)
}

// Progress rewrites one transient status line on a terminal. It writes nothing elsewhere.
type Progress struct{ output *Output }

// NewProgress returns a transient line writer for output.
func NewProgress(output *Output) *Progress { return &Progress{output: output} }

// Update replaces the current progress line.
func (p *Progress) Update(text string) {
	if p == nil || !p.output.terminal {
		return
	}
	if p.output.control("\r"+ansi.EraseEntireLine) == nil {
		_, _ = p.output.WriteString(Faint(ansi.Truncate(text, p.output.width-1, "…")))
	}
}

// Clear removes the progress line.
func (p *Progress) Clear() {
	if p == nil || !p.output.terminal {
		return
	}
	_ = p.output.control("\r" + ansi.EraseEntireLine)
}

// Wrap breaks text into lines of at most width cells and prefixes each line with indent.
func Wrap(text string, width int, indent string) string {
	limit := max(width-lipgloss.Width(indent), 20)
	lines := strings.Split(lipgloss.Wrap(text, limit, ""), "\n")
	for i, line := range lines {
		lines[i] = indent + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// Indent prefixes every non-empty line of text.
func Indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// Width returns the display width of text, ignoring escape sequences.
func Width(text string) int { return lipgloss.Width(text) }

// Item renders a marked list entry wrapped to width. Continuation lines align with the text.
func Item(t Tone, text string, width int, indent string) string {
	return indent + Symbol(t) + " " + strings.TrimPrefix(Wrap(text, width, indent+"  "), indent+"  ")
}
