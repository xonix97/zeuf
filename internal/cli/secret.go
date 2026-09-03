package cli

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// promptSecret reads a line without echo (for pasting API keys).
func promptSecret(prompt string) string {
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
