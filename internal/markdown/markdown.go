// Package markdown implements a small, dependency-free and XSS-safe Markdown to
// HTML renderer. All input is HTML-escaped before any formatting is applied, so
// raw HTML in the source can never reach the output. Only a limited, safe subset
// of Markdown is supported (headings, emphasis, code, links, lists, blockquotes,
// horizontal rules and paragraphs), which is sufficient for admin-authored home
// page content.
package markdown

import (
	"html"
	"regexp"
	"strings"
)

var (
	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRe = regexp.MustCompile(`\*([^*]+)\*`)
	codeRe   = regexp.MustCompile("`([^`]+)`")
	// linkRe matches [text](url); both parts are already HTML-escaped at this point.
	linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

// Render converts Markdown source into safe HTML.
func Render(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")

	var out strings.Builder
	var para []string
	listType := "" // "ul" or "ol" when inside a list
	inCode := false
	var code []string

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		text := strings.Join(para, "<br>")
		out.WriteString("<p>" + inline(text) + "</p>\n")
		para = nil
	}
	flushList := func() {
		if listType != "" {
			out.WriteString("</" + listType + ">\n")
			listType = ""
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced code blocks.
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				out.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>\n")
				code = nil
				inCode = false
			} else {
				flushPara()
				flushList()
				inCode = true
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}

		if trimmed == "" {
			flushPara()
			flushList()
			continue
		}

		// Horizontal rule.
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			flushPara()
			flushList()
			out.WriteString("<hr>\n")
			continue
		}

		// Headings (# .. ######).
		if m := headingLevel(trimmed); m > 0 {
			flushPara()
			flushList()
			content := strings.TrimSpace(trimmed[m:])
			tag := "h" + string(rune('0'+m))
			out.WriteString("<" + tag + ">" + inline(html.EscapeString(content)) + "</" + tag + ">\n")
			continue
		}

		// Blockquote.
		if strings.HasPrefix(trimmed, "> ") || trimmed == ">" {
			flushPara()
			flushList()
			content := strings.TrimPrefix(trimmed, ">")
			content = strings.TrimPrefix(content, " ")
			out.WriteString("<blockquote>" + inline(html.EscapeString(content)) + "</blockquote>\n")
			continue
		}

		// Unordered list.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			flushPara()
			if listType != "ul" {
				flushList()
				out.WriteString("<ul>\n")
				listType = "ul"
			}
			out.WriteString("<li>" + inline(html.EscapeString(strings.TrimSpace(trimmed[2:]))) + "</li>\n")
			continue
		}

		// Ordered list (e.g. "1. item").
		if isOrderedItem(trimmed) {
			flushPara()
			if listType != "ol" {
				flushList()
				out.WriteString("<ol>\n")
				listType = "ol"
			}
			idx := strings.Index(trimmed, ". ")
			out.WriteString("<li>" + inline(html.EscapeString(strings.TrimSpace(trimmed[idx+2:]))) + "</li>\n")
			continue
		}

		// Regular paragraph text (escaped now, inline formatting applied on flush).
		flushList()
		para = append(para, html.EscapeString(trimmed))
	}

	if inCode {
		out.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>\n")
	}
	flushPara()
	flushList()
	return strings.TrimSpace(out.String())
}

func headingLevel(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n >= 1 && n <= 6 && n < len(s) && s[n] == ' ' {
		return n
	}
	return 0
}

func isOrderedItem(s string) bool {
	idx := strings.Index(s, ". ")
	if idx <= 0 {
		return false
	}
	for _, c := range s[:idx] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// inline applies inline formatting to already HTML-escaped text. Link URLs are
// validated against a scheme allowlist to avoid javascript: and similar vectors.
func inline(s string) string {
	s = codeRe.ReplaceAllString(s, "<code>$1</code>")
	s = boldRe.ReplaceAllString(s, "<strong>$1</strong>")
	s = italicRe.ReplaceAllString(s, "<em>$1</em>")
	s = linkRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		text, url := parts[1], parts[2]
		if !safeURL(url) {
			return text
		}
		return `<a href="` + url + `" target="_blank" rel="noopener noreferrer">` + text + `</a>`
	})
	return s
}

func safeURL(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	if strings.HasPrefix(lower, "mailto:") {
		return true
	}
	// Allow site-relative links.
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, "#") {
		return true
	}
	return false
}
