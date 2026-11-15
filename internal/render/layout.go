package render

import (
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
)

// Field is one label and value in an aligned list.
type Field struct {
	Label string
	Value string
}

// Fields aligns labels in a column. Values may contain styled text and line breaks.
func Fields(fields []Field, indent string) string {
	return AlignedFields(fields, indent, LabelWidth(fields))
}

// LabelWidth returns the widest label across groups, so separate sections can share one value column.
func LabelWidth(groups ...[]Field) int {
	width := 0
	for _, fields := range groups {
		for _, f := range fields {
			width = max(width, lipgloss.Width(f.Label))
		}
	}
	return width
}

// AlignedFields renders fields with a fixed label column width.
func AlignedFields(fields []Field, indent string, labelWidth int) string {
	var b strings.Builder
	for _, f := range fields {
		pad := strings.Repeat(" ", max(labelWidth-lipgloss.Width(f.Label), 0)+2)
		continuation := indent + strings.Repeat(" ", labelWidth+2)
		value := strings.ReplaceAll(f.Value, "\n", "\n"+continuation)
		b.WriteString(indent + Faint(f.Label) + pad + value + "\n")
	}
	return b.String()
}

// Table renders a borderless table with emphasised headers, like kubectl output.
func Table(headers []string, rows [][]string, indent string) string {
	if len(rows) == 0 {
		return ""
	}
	t := table.New().
		Headers(headers...).
		Rows(rows...).
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle()
			if col < len(headers)-1 {
				style = style.PaddingRight(2)
			}
			if row == table.HeaderRow {
				style = style.Bold(true)
			}
			return style
		})
	lines := strings.Split(t.String(), "\n")
	for i, line := range lines {
		lines[i] = indent + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// Node is one entry in an ownership or containment tree.
type Node struct {
	Label    string
	Children []Node
}

// Tree renders nodes with rounded branch connectors.
func Tree(root Node, indent string) string {
	rendered := build(root).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(lipgloss.NewStyle().Faint(true).PaddingRight(1)).
		IndenterStyle(lipgloss.NewStyle().Faint(true).PaddingRight(1)).
		String()
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = indent + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

func build(node Node) *tree.Tree {
	t := tree.Root(node.Label)
	for _, child := range node.Children {
		if len(child.Children) == 0 {
			t.Child(child.Label)
			continue
		}
		t.Child(build(child))
	}
	return t
}
