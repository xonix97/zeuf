package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// This file renders assistant markdown for the terminal: headings, bold,
// lists, tables (via glamour) plus fenced/inline code and LaTeX math
// spans ($…$ / $$…$$), which terminals cannot typeset but should display
// distinctly instead of as noise.
//
// Math handling: spans are extracted to placeholders before glamour runs
// (goldmark would otherwise leave them unstyled or, inside emphasis,
// mangle them), then restored with math styling. Code spans and fenced
// blocks are never treated as math.

var mathStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Italic(true)

type mathSpan struct {
	placeholder string
	styled      string
}

var (
	reDisplayMath = regexp.MustCompile(`(?s)\$\$(.+?)\$\$`)
	reInlineMath  = regexp.MustCompile(`\$([^$\n]+?)\$`)
)

// extractMathSpans pulls LaTeX spans out of non-code markdown, returning
// redacted text plus styled replacements keyed by placeholder.
func extractMathSpans(src string) (string, []mathSpan) {
	lines := strings.Split(src, "\n")
	var spans []mathSpan
	take := func(raw string) string {
		ph := placeholder(len(spans))
		spans = append(spans, mathSpan{placeholder: ph, styled: mathStyle.Render(raw)})
		return ph
	}
	// Pass 1: display math across the whole source (outside fences).
	out := splitFences(strings.Join(lines, "\n"), func(textSeg string, inCode bool) string {
		if inCode {
			return textSeg
		}
		return reDisplayMath.ReplaceAllStringFunc(textSeg, func(m string) string {
			inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "$$"), "$$"))
			if inner == "" {
				return m
			}
			return take("$$" + inner + "$$")
		})
	})
	// Pass 2: inline math per line, skipping backquote code spans. A
	// manual scan (not FindAll) so a rejected match's closing "$" can
	// still open the real span ("`$HOME` then $x^2$").
	ls := strings.Split(out, "\n")
	inFence := false
	for i, ln := range ls {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		var b strings.Builder
		pos := 0
		for pos < len(ln) {
			if ln[pos] != '$' || insideBackquotes(ln, pos) {
				b.WriteByte(ln[pos])
				pos++
				continue
			}
			closing := -1
			for k := pos + 1; k < len(ln); k++ {
				if ln[k] == '$' && !insideBackquotes(ln, k) {
					closing = k
					break
				}
			}
			if closing < 0 {
				b.WriteByte(ln[pos])
				pos++
				continue
			}
			inner := ln[pos+1 : closing]
			if validMathInner(inner) {
				b.WriteString(take("$" + inner + "$"))
				pos = closing + 1
			} else {
				b.WriteByte(ln[pos])
				pos++
			}
		}
		ls[i] = b.String()
	}
	return strings.Join(ls, "\n"), spans
}

// validMathInner rejects currency and empty spans: "$5 and $6" fails on
// the trailing space, "$ x$" on the leading one.
func validMathInner(inner string) bool {
	if inner == "" || inner[0] == ' ' || inner[len(inner)-1] == ' ' {
		return false
	}
	return strings.TrimSpace(inner) != ""
}

// insideBackquotes reports whether byte offset sits inside an inline code
// span on the same line (odd count of unescaped backquotes before it).
func insideBackquotes(line string, off int) bool {
	if off < 0 {
		return false
	}
	n := 0
	for i := 0; i < off && i < len(line); i++ {
		if line[i] == '`' && (i == 0 || line[i-1] != '\\') {
			n++
		}
	}
	return n%2 == 1
}

// splitFences maps over alternating text/fence segments, tracking ``` blocks.
func splitFences(src string, fn func(seg string, inCode bool) string) string {
	lines := strings.Split(src, "\n")
	var cur []string
	var out []string
	inCode := false
	flush := func() {
		out = append(out, fn(strings.Join(cur, "\n"), inCode))
		cur = nil
	}
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			flush()
			out = append(out, ln)
			inCode = !inCode
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return strings.Join(out, "\n")
}

func placeholder(i int) string { return "⟦ZM" + itoa(i) + "⟧" }

// repairMarkdown leniently fixes the sloppy markdown models emit so code
// still renders as code: unclosed fenced blocks are closed at EOF, and a
// lone backtick around a bare token (path, command, identifier) is paired.
// Balanced lines and prose are never touched; raw block text is preserved
// and only the cooked display copy is repaired.
func repairMarkdown(src string) string {
	lines := strings.Split(src, "\n")
	openFence := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			openFence = !openFence
			continue
		}
		if openFence {
			continue
		}
		lines[i] = repairLineTicks(ln)
	}
	out := strings.Join(lines, "\n")
	if openFence {
		out += "\n```"
	}
	return out
}

// repairLineTicks pairs a lone backtick that wraps (or starts) a bare
// token. Both rules require a spaceless tail so prose is never rewritten.
func repairLineTicks(ln string) string {
	if countTicks(ln) != 1 {
		// Odd counts of 3+: leave alone (ambiguous); even: balanced.
		if countTicks(ln)%2 == 0 {
			return ln
		}
		return closeTrailingOpener(ln)
	}
	idx := strings.LastIndex(ln, "`")
	tail := ln[idx+1:]
	if strings.TrimSpace(tail) == "" {
		// Lone trailing tick: "`/path`" missing its opener, or "`cmd`"
		// missing it — pair forward from the content start when the
		// content is a bare token ("> /home/x`" → "> `/home/x`").
		prefix, content := splitPrefix(ln[:idx])
		token := strings.TrimSpace(strings.TrimSuffix(content, "`"))
		if token == "" || strings.Contains(token, " ") || strings.Contains(token, "`") {
			return ln
		}
		return prefix + "`" + token + "`"
	}
	// Lone opener with a spaceless tail ("path `/x") — close at EOL.
	if !strings.Contains(tail, " ") && strings.TrimSpace(tail) != "" {
		return ln + "`"
	}
	return ln
}

// closeTrailingOpener handles 3+ odd ticks by closing at EOL only when the
// tail is spaceless; otherwise the line is left raw.
func closeTrailingOpener(ln string) string {
	idx := strings.LastIndex(ln, "`")
	tail := ln[idx+1:]
	if !strings.Contains(tail, " ") && strings.TrimSpace(tail) != "" {
		return ln + "`"
	}
	return ln
}

// countTicks counts unescaped backticks outside inline code context is
// irrelevant here — a raw per-line count is what pairing needs.
func countTicks(ln string) int {
	n := 0
	for i := 0; i < len(ln); i++ {
		if ln[i] == '`' && (i == 0 || ln[i-1] != '\\') {
			n++
		}
	}
	return n
}

// splitPrefix separates leading indent/quote/list markers from content.
func splitPrefix(ln string) (prefix, content string) {
	i := 0
	for i < len(ln) && (ln[i] == ' ' || ln[i] == '\t') {
		i++
	}
	if i < len(ln) && ln[i] == '>' {
		i++
		if i < len(ln) && ln[i] == ' ' {
			i++
		}
	}
	for i < len(ln) && (ln[i] == ' ' || ln[i] == '\t') {
		i++
	}
	rest := ln[i:]
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && rest[1] == ' ' {
		i += 2
	} else {
		j := i
		for j < len(ln) && ln[j] >= '0' && ln[j] <= '9' {
			j++
		}
		if j > i && j < len(ln) && (ln[j] == '.' || ln[j] == ')') {
			j++
			if j < len(ln) && ln[j] == ' ' {
				j++
			}
			i = j
		}
	}
	return ln[:i], ln[i:]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// cookMarkdown renders markdown to ANSI at width, preserving LaTeX spans
// with math styling. On any renderer failure it falls back to plain wrap.
func cookMarkdown(src string, width int) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	if width < 20 {
		width = 80
	}
	src = repairMarkdown(src)
	redacted, spans := extractMathSpans(src)
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return strings.Join(wrapLines(src, width), "\n")
	}
	rendered, err := r.Render(redacted)
	if err != nil {
		return strings.Join(wrapLines(src, width), "\n")
	}
	out := strings.TrimRight(rendered, "\n")
	for _, s := range spans {
		out = strings.ReplaceAll(out, s.placeholder, s.styled)
	}
	return out
}
