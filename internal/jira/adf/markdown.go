package adf

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func ToMarkdown(node map[string]any) string {
	nodeType, _ := node["type"].(string)

	switch nodeType {
	case "doc":
		return renderChildren(node)

	case "paragraph":
		return renderChildren(node) + "\n\n"

	case "heading":
		level := intAttr(node, "level", 1)
		prefix := strings.Repeat("#", level)
		return prefix + " " + renderChildren(node) + "\n\n"

	case "text":
		text, _ := node["text"].(string)
		return applyMarks(text, node)

	case "hardBreak":
		return "\n"

	case "rule":
		return "---\n\n"

	case "bulletList":
		return renderList(node, false)

	case "orderedList":
		return renderList(node, true)

	case "listItem":
		return renderChildren(node)

	case "codeBlock":
		lang, _ := stringAttr(node, "language")
		body := renderChildren(node)
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return "```" + lang + "\n" + body + "```\n\n"

	case "blockquote":
		inner := renderChildren(node)
		lines := strings.Split(strings.TrimRight(inner, "\n"), "\n")
		var b strings.Builder
		for _, line := range lines {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		return b.String()

	case "inlineCard":
		if u, ok := stringAttr(node, "url"); ok {
			return fmt.Sprintf("[%s](%s)", u, u)
		}
		return ""

	case "mention":
		text, _ := stringAttr(node, "text")
		return text

	case "emoji":
		name, _ := stringAttr(node, "shortName")
		return name

	case "date":
		ts, _ := stringAttr(node, "timestamp")
		ms, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return ts
		}
		return time.UnixMilli(ms).UTC().Format("2006-01-02")

	case "status":
		text, _ := stringAttr(node, "text")
		return "[" + text + "]"

	case "panel":
		return renderPanel(node)

	case "expand", "nestedExpand":
		title, _ := stringAttr(node, "title")
		var b strings.Builder
		if title != "" {
			b.WriteString("**" + title + "**\n\n")
		}
		b.WriteString(renderChildren(node))
		return b.String()

	case "table":
		return renderTable(node)

	case "mediaSingle", "mediaGroup":
		return renderChildren(node)

	case "media", "mediaInline":
		if u, ok := stringAttr(node, "url"); ok {
			return fmt.Sprintf("[media](%s)", u)
		}
		return "[media]"

	default:
		// Graceful fallback: recurse into content children.
		return renderChildren(node)
	}
}

func renderChildren(node map[string]any) string {
	content, ok := node["content"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, child := range content {
		if childMap, ok := child.(map[string]any); ok {
			b.WriteString(ToMarkdown(childMap))
		}
	}
	return b.String()
}

func renderList(node map[string]any, ordered bool) string {
	content, ok := node["content"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for i, child := range content {
		childMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		inner := strings.TrimRight(renderChildren(childMap), "\n")
		if ordered {
			fmt.Fprintf(&b, "%d. %s\n", i+1, inner)
		} else {
			fmt.Fprintf(&b, "- %s\n", inner)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func applyMarks(text string, node map[string]any) string {
	marks, ok := node["marks"].([]any)
	if !ok {
		return text
	}
	for _, m := range marks {
		mark, ok := m.(map[string]any)
		if !ok {
			continue
		}
		markType, _ := mark["type"].(string)
		switch markType {
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "*" + text + "*"
		case "code":
			text = "`" + text + "`"
		case "strike":
			text = "~~" + text + "~~"
		case "link":
			if href, ok := stringAttr(mark, "href"); ok {
				text = fmt.Sprintf("[%s](%s)", text, href)
			}
		}
	}
	return text
}

func stringAttr(node map[string]any, key string) (string, bool) {
	attrs, ok := node["attrs"].(map[string]any)
	if !ok {
		return "", false
	}
	val, ok := attrs[key].(string)
	return val, ok
}

func intAttr(node map[string]any, key string, fallback int) int {
	attrs, ok := node["attrs"].(map[string]any)
	if !ok {
		return fallback
	}
	switch v := attrs[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return fallback
	}
}

func renderPanel(node map[string]any) string {
	panelType, _ := stringAttr(node, "panelType")
	label := panelTypeLabel(panelType)
	inner := renderChildren(node)
	lines := strings.Split(strings.TrimRight(inner, "\n"), "\n")
	var b strings.Builder
	b.WriteString("> **" + label + "**\n")
	for _, line := range lines {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func panelTypeLabel(panelType string) string {
	switch panelType {
	case "info":
		return "Info:"
	case "note":
		return "Note:"
	case "warning":
		return "Warning:"
	case "error":
		return "Error:"
	case "success":
		return "Success:"
	default:
		return "Note:"
	}
}

func renderTable(node map[string]any) string {
	rows := nodeChildren(node)
	if len(rows) == 0 {
		return ""
	}

	var b strings.Builder
	firstRow := rows[0]
	firstCells := nodeChildren(firstRow)
	isHeader := len(firstCells) > 0 && nodeType(firstCells[0]) == "tableHeader"

	// Render first row
	b.WriteString(renderTableRow(firstCells))
	// Separator
	b.WriteString("|")
	for range firstCells {
		b.WriteString("---|")
	}
	b.WriteString("\n")

	// Remaining rows
	for _, row := range rows[1:] {
		cells := nodeChildren(row)
		b.WriteString(renderTableRow(cells))
	}

	// If first row was not a header, we still used it as the header line already
	_ = isHeader
	b.WriteString("\n")
	return b.String()
}

func renderTableRow(cells []map[string]any) string {
	var b strings.Builder
	b.WriteString("|")
	for _, cell := range cells {
		b.WriteString(" ")
		b.WriteString(renderCellInline(cell))
		b.WriteString(" |")
	}
	b.WriteString("\n")
	return b.String()
}

func renderCellInline(cell map[string]any) string {
	inner := renderChildren(cell)
	// Strip trailing paragraph newlines for inline cell rendering
	return strings.TrimRight(inner, "\n")
}

func nodeChildren(node map[string]any) []map[string]any {
	content, ok := node["content"].([]any)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, child := range content {
		if childMap, ok := child.(map[string]any); ok {
			result = append(result, childMap)
		}
	}
	return result
}

func nodeType(node map[string]any) string {
	t, _ := node["type"].(string)
	return t
}
