package markdown

import (
	"strings"
	"testing"
)

func TestRenderBasics(t *testing.T) {
	out := Render("# Title\n\nHello **world** and *you*\n\n- a\n- b")
	for _, want := range []string{"<h1>Title</h1>", "<strong>world</strong>", "<em>you</em>", "<ul>", "<li>a</li>", "<li>b</li>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderEscapesHTML(t *testing.T) {
	out := Render("<script>alert(1)</script>")
	if strings.Contains(out, "<script>") {
		t.Fatalf("raw script tag leaked: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag, got: %s", out)
	}
}

func TestRenderRejectsJavascriptLinks(t *testing.T) {
	out := Render("[click](javascript:alert(1))")
	if strings.Contains(strings.ToLower(out), "href=\"javascript") {
		t.Fatalf("javascript link not sanitized: %s", out)
	}
	if !strings.Contains(out, "click") {
		t.Fatalf("expected link text to remain: %s", out)
	}
}

func TestRenderAllowsSafeLinks(t *testing.T) {
	out := Render("[site](https://example.com)")
	if !strings.Contains(out, `href="https://example.com"`) {
		t.Fatalf("safe link missing: %s", out)
	}
	if !strings.Contains(out, `rel="noopener noreferrer"`) {
		t.Fatalf("expected rel attribute: %s", out)
	}
}

func TestRenderCodeBlock(t *testing.T) {
	out := Render("```\n<b>x</b>\n```")
	if !strings.Contains(out, "<pre><code>") || strings.Contains(out, "<b>x</b>") {
		t.Fatalf("code block not escaped properly: %s", out)
	}
}
