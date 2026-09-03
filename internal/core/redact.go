package core

import "regexp"

var (
	reBearer  = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+\-/=]{8,}`)
	reSkKey   = regexp.MustCompile(`\b(sk-[A-Za-z0-9._\-]{8,}|xox[bpas]-[A-Za-z0-9\-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|AIza[A-Za-z0-9_\-]{8,})`)
	reKeyVal  = regexp.MustCompile(`(?i)((?:api[_-]?key|apikey|token|secret|password)\s*[:=]\s*['"]?)([^'"\s]{6,})(['"]?)`)
	reEnvLine = regexp.MustCompile(`(?i)^([A-Z_0-9]*(?:KEY|TOKEN|SECRET|PASSWORD)[A-Z_0-9]*)=(.+)$`)
)

// Redact returns s with probable credentials masked. Use it for every log,
// error surface, and `doctor` output. It errs on the side of masking.
func Redact(s string) string {
	s = reBearer.ReplaceAllString(s, "${1}***")
	s = reSkKey.ReplaceAllString(s, "***")
	s = reKeyVal.ReplaceAllString(s, "${1}***${3}")
	lines := splitLines(s)
	for i, ln := range lines {
		if m := reEnvLine.FindStringSubmatch(ln); m != nil {
			lines[i] = m[1] + "=***"
		}
	}
	return joinLines(lines)
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
