package tui

import (
	"regexp"
	"strings"
	"testing"
)

func TestExtractInlineMath(t *testing.T) {
	out, spans := extractMathSpans("Energy $E=mc^2$ here.")
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if strings.Contains(out, "$E=mc^2$") {
		t.Errorf("math left in place: %q", out)
	}
	if !strings.Contains(spans[0].styled, "E=mc^2") {
		t.Errorf("styled math lost content: %q", spans[0].styled)
	}
}

func TestCurrencyNotMath(t *testing.T) {
	for _, s := range []string{"costs $5 and $6 each", "a $ b", "$ spaced $", "$$  $$"} {
		_, spans := extractMathSpans(s)
		if len(spans) != 0 {
			t.Errorf("%q produced %d spans", s, len(spans))
		}
	}
}

func TestDisplayMath(t *testing.T) {
	src := "Then:\n$$\\int_0^1 x\\,dx$$\nDone."
	out, spans := extractMathSpans(src)
	if len(spans) != 1 {
		t.Fatalf("spans = %d: %q", len(spans), out)
	}
}

func TestFenceNotMath(t *testing.T) {
	src := "```sh\necho $HOME $$X$$\n```\nReal $a^2$ here."
	out, spans := extractMathSpans(src)
	if len(spans) != 1 {
		t.Fatalf("spans = %d: %q", len(spans), out)
	}
	if !strings.Contains(out, "$HOME") || !strings.Contains(out, "$$X$$") {
		t.Errorf("fence content altered: %q", out)
	}
}

func TestInlineCodeNotMath(t *testing.T) {
	src := "Run `$HOME/bin` then $x^2$."
	_, spans := extractMathSpans(src)
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if !strings.Contains(spans[0].styled, "x^2") {
		t.Errorf("wrong span styled: %+v", spans)
	}
}

func TestCookMarkdown(t *testing.T) {
	src := "# Fix\n\nUse `fmt.Println` and recall $E=mc^2$.\n\n```go\nfmt.Println(\"hi\")\n```\n\n$$a^2+b^2$$\n"
	out := cookMarkdown(src, 80)
	for _, want := range []string{"Fix", "fmt.Println", "E=mc^2", "a^2+b^2"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "⟦ZM") {
		t.Errorf("placeholder leaked:\n%s", out)
	}
}

func TestCookEmpty(t *testing.T) {
	if cookMarkdown("   \n ", 80) != "" {
		t.Error("empty input must render empty")
	}
}

func TestMathStyledEndToEnd(t *testing.T) {
	out := cookMarkdown("Recall $E=mc^2$ soon.", 80)
	if strings.Contains(out, "⟦ZM") {
		t.Errorf("placeholder leaked:\n%s", out)
	}
	if !strings.Contains(out, "E=mc^2") {
		t.Errorf("math content lost:\n%s", out)
	}
	if !strings.Contains(out, "213") {
		t.Errorf("math not styled distinctly (want magenta 213):\n%q", out)
	}
}

// TestLenientCodeRepair pins the sloppy-model fixes: a trailing lone tick
// around a bare token, an unclosed opener, and an unclosed fence must all
// still render as code, while prose stays raw.
func TestLenientCodeRepair(t *testing.T) {
	cooked := func(s string) string { return stripANSITest(cookMarkdown(s, 60)) }
	if got := cooked("> /home/archlinux`"); strings.Contains(got, "`") || !strings.Contains(got, "/home/archlinux") {
		t.Errorf("trailing lone tick not repaired: %q", got)
	}
	if got := cooked("path `/home/archlinux"); strings.Contains(got, "`") {
		t.Errorf("unclosed opener not repaired: %q", got)
	}
	if got := cooked("```sh\necho hi"); strings.Contains(got, "```") || !strings.Contains(got, "echo hi") {
		t.Errorf("unclosed fence not repaired: %q", got)
	}
	if got := cooked("The dir is `/home/archlinux`."); strings.Contains(got, "`") {
		t.Errorf("balanced code altered: %q", got)
	}
	if got := cooked("Its `five o clock here"); !strings.Contains(got, "`") {
		t.Errorf("spaced prose must stay raw: %q", got)
	}
}

var ansiReTest = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07")

func stripANSITest(s string) string { return ansiReTest.ReplaceAllString(s, "") }
