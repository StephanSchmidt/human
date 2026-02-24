package adf

import (
	"fmt"
	"strings"
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
		return "```" + lang + "\n" + renderChildren(node) + "```\n\n"

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
	val, ok := attrs[key].(float64)
	if !ok {
		return fallback
	}
	return int(val)
}
