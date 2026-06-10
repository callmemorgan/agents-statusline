package main

import (
	_ "embed"
	"regexp"
	"strings"

	"github.com/rivo/tview"
)

//go:embed README.md
var readmeContent string

// ─── Markdown → tview renderer ───────────────────────────────────────

var (
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
)

func inlineMarkdown(s string) string {
	s = reBold.ReplaceAllString(s, "[::b]$1[::-]")
	s = reInlineCode.ReplaceAllString(s, "[green]$1[-]")
	return s
}

func markdownToTview(md string) string {
	var b strings.Builder
	inCode := false
	for _, line := range strings.Split(md, "\n") {
		// Code fence open/close.
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			if inCode {
				b.WriteString("[gray]")
			} else {
				b.WriteString("[-]\n")
			}
			continue
		}
		if inCode {
			b.WriteString(tview.Escape(line) + "\n")
			continue
		}

		esc := tview.Escape(line)
		switch {
		case strings.HasPrefix(line, "# "):
			b.WriteString("\n[yellow::b]" + tview.Escape(strings.TrimPrefix(line, "# ")) + "[-::-]\n")
		case strings.HasPrefix(line, "## "):
			b.WriteString("\n[cyan::b]  " + tview.Escape(strings.TrimPrefix(line, "## ")) + "[-::-]\n")
		case strings.HasPrefix(line, "### "):
			b.WriteString("[green::b]    " + tview.Escape(strings.TrimPrefix(line, "### ")) + "[-::-]\n")
		case strings.HasPrefix(line, "|"):
			// Table rows and separators — dim.
			b.WriteString("[::d]" + esc + "[-::-]\n")
		case strings.HasPrefix(line, "---"):
			// Horizontal rule.
			b.WriteString("[::d]────────────────────────────────────────[-::-]\n")
		default:
			b.WriteString(inlineMarkdown(esc) + "\n")
		}
	}
	return b.String()
}

// ansiToTview converts ANSI color codes in s to tview color tags.
// It escapes literal '[' characters so they are not interpreted as tags.
// Only the specific SGR codes emitted by currentPalette() are mapped; any
// other ANSI sequences (bold, 256-color, etc.) will pass through as raw bytes.
func ansiToTview(s string) string {
	// Step 1: replace ANSI codes with Unicode private-use placeholders.
	s = strings.ReplaceAll(s, "\x1b[0m", "\uE000")
	s = strings.ReplaceAll(s, "\x1b[30m", "\uE010")
	s = strings.ReplaceAll(s, "\x1b[31m", "\uE011")
	s = strings.ReplaceAll(s, "\x1b[32m", "\uE012")
	s = strings.ReplaceAll(s, "\x1b[33m", "\uE013")
	s = strings.ReplaceAll(s, "\x1b[34m", "\uE014")
	s = strings.ReplaceAll(s, "\x1b[35m", "\uE015")
	s = strings.ReplaceAll(s, "\x1b[36m", "\uE016")
	s = strings.ReplaceAll(s, "\x1b[37m", "\uE017")
	s = strings.ReplaceAll(s, "\x1b[90m", "\uE020")
	s = strings.ReplaceAll(s, "\x1b[91m", "\uE021")
	s = strings.ReplaceAll(s, "\x1b[92m", "\uE022")
	s = strings.ReplaceAll(s, "\x1b[93m", "\uE023")
	s = strings.ReplaceAll(s, "\x1b[94m", "\uE024")
	s = strings.ReplaceAll(s, "\x1b[95m", "\uE025")
	s = strings.ReplaceAll(s, "\x1b[96m", "\uE026")
	s = strings.ReplaceAll(s, "\x1b[97m", "\uE027")

	// Step 2: escape literal '[' for tview.
	s = tview.Escape(s)

	// Step 3: replace placeholders with tview color tags.
	s = strings.ReplaceAll(s, "\uE000", "[-]")
	s = strings.ReplaceAll(s, "\uE010", "[black]")
	s = strings.ReplaceAll(s, "\uE011", "[red]")
	s = strings.ReplaceAll(s, "\uE012", "[green]")
	s = strings.ReplaceAll(s, "\uE013", "[yellow]")
	s = strings.ReplaceAll(s, "\uE014", "[blue]")
	s = strings.ReplaceAll(s, "\uE015", "[magenta]")
	s = strings.ReplaceAll(s, "\uE016", "[cyan]")
	s = strings.ReplaceAll(s, "\uE017", "[white]")
	s = strings.ReplaceAll(s, "\uE020", "[gray]")
	s = strings.ReplaceAll(s, "\uE021", "[red::b]")
	s = strings.ReplaceAll(s, "\uE022", "[green::b]")
	s = strings.ReplaceAll(s, "\uE023", "[yellow::b]")
	s = strings.ReplaceAll(s, "\uE024", "[blue::b]")
	s = strings.ReplaceAll(s, "\uE025", "[magenta::b]")
	s = strings.ReplaceAll(s, "\uE026", "[cyan::b]")
	s = strings.ReplaceAll(s, "\uE027", "[white::b]")

	return s
}
