package render

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Tone classifies a fragment by meaning. Views choose tones; this package chooses colours and symbols.
type Tone int

const (
	Neutral Tone = iota
	Good
	Warn
	Bad
	Unknown
	Info
)

// Basic ANSI colours follow the user's terminal theme on light and dark backgrounds.
func (t Tone) color() color.Color {
	switch t {
	case Good:
		return lipgloss.Green
	case Warn:
		return lipgloss.Yellow
	case Bad:
		return lipgloss.Red
	case Unknown:
		return lipgloss.Magenta
	case Info:
		return lipgloss.Cyan
	default:
		return nil
	}
}

// Symbol is a one-cell marker for a tone. Symbols carry meaning without colour.
func Symbol(t Tone) string {
	switch t {
	case Good:
		return Paint(t, "✓")
	case Warn:
		return Paint(t, "!")
	case Bad:
		return Paint(t, "✗")
	case Unknown:
		return Paint(t, "?")
	case Info:
		return Paint(t, "•")
	default:
		return "•"
	}
}

// Paint colours text by tone.
func Paint(t Tone, text string) string {
	c := t.color()
	if c == nil || text == "" {
		return text
	}
	return lipgloss.NewStyle().Foreground(c).Render(text)
}

// Status renders a symbol and bold, coloured label, such as a headline verdict.
func Status(t Tone, label string) string {
	style := lipgloss.NewStyle().Bold(true)
	if c := t.color(); c != nil {
		style = style.Foreground(c)
	}
	return Symbol(t) + " " + style.Render(label)
}

// Bold emphasises text.
func Bold(text string) string {
	if text == "" {
		return text
	}
	return lipgloss.NewStyle().Bold(true).Render(text)
}

// Faint de-emphasises secondary text such as identifiers, timestamps, and caveats.
func Faint(text string) string {
	if text == "" {
		return text
	}
	return lipgloss.NewStyle().Faint(true).Render(text)
}

// Command marks a command the reader can copy.
func Command(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Cyan).Render(text)
}

// Heading renders a section title.
func Heading(text string) string { return Bold(text) }

// Next renders a suggested next step.
func Next(text string) string { return Paint(Info, "→") + " " + text }

// NextCommand renders a suggested next step with its command on a separate line, so it can be copied whole.
func NextCommand(text, command, indent string) string {
	return indent + Next(text) + "\n" + indent + "    " + Command(command) + "\n"
}
