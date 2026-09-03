package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Force truecolor so style assertions are deterministic: without a TTY,
// lipgloss would strip all ANSI and styled-vs-plain would be invisible.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
