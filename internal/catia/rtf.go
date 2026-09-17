package catia

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Destination groups whose content is formatting, not text.
var rtfSkippedGroups = []string{"fonttbl", "colortbl", "stylesheet", "info"}

// rtfPlain returns the plain text of an RTF string: skipped destination groups and control words
// removed, \par and \line as line breaks, lines trimmed and empty lines dropped.
func rtfPlain(s string) string {
	var b strings.Builder
	depth, skipAt := 0, 0 // skipAt is the depth of the skipped group, 0 when none
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '{':
			depth++
			i++
			if skipAt == 0 && rtfSkippedGroup(s[i:]) {
				skipAt = depth
			}
		case '}':
			if depth == skipAt {
				skipAt = 0
			}
			depth = max(depth-1, 0)
			i++
		case '\\':
			i = rtfControl(s, i, &b, skipAt != 0)
		case '\r', '\n':
			i++
		default:
			if skipAt == 0 {
				_ = b.WriteByte(c)
			}
			i++
		}
	}
	return plainLines(b.String())
}

// rtfSkippedGroup reports whether a group whose content starts with rest is a skipped destination.
func rtfSkippedGroup(rest string) bool {
	if strings.HasPrefix(rest, `\*`) {
		return true
	}
	word, ok := strings.CutPrefix(rest, `\`)
	if !ok {
		return false
	}
	for _, g := range rtfSkippedGroups {
		if strings.HasPrefix(word, g) && (len(word) == len(g) || !isASCIILetter(word[len(g)])) {
			return true
		}
	}
	return false
}

// rtfControl handles the control word or symbol at s[i] (a backslash) and returns the index after
// it. Text is written only when not skipping.
func rtfControl(s string, i int, b *strings.Builder, skip bool) int {
	write := func(text string) {
		if !skip {
			_, _ = b.WriteString(text)
		}
	}
	if i+1 >= len(s) {
		return len(s)
	}
	switch next := s[i+1]; {
	case next == '\\' || next == '{' || next == '}':
		write(s[i+1 : i+2])
		return i + 2
	case next == '\'':
		if i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				if v < 0x80 {
					write(string(rune(v)))
				}
				return i + 4
			}
		}
		return i + 2
	case !isASCIILetter(next):
		return i + 2
	}
	j := i + 1
	for j < len(s) && isASCIILetter(s[j]) {
		j++
	}
	word := s[i+1 : j]
	k := j
	if k < len(s) && s[k] == '-' {
		k++
	}
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	param := s[j:k]
	if param == "-" {
		k, param = j, ""
	}
	if k < len(s) && s[k] == ' ' {
		k++
	}
	switch word {
	case "par", "line":
		write("\n")
	case "tab":
		write(" ")
	case "u":
		if n, err := strconv.Atoi(param); err == nil {
			if n < 0 {
				n += 65536
			}
			write(string(rune(n)))
			return rtfSkipFallback(s, k)
		}
	}
	return k
}

// rtfSkipFallback skips the one fallback character that follows a \u control word.
func rtfSkipFallback(s string, k int) int {
	switch {
	case k >= len(s) || s[k] == '{' || s[k] == '}':
		return k
	case strings.HasPrefix(s[k:], `\'`):
		return min(k+4, len(s))
	case s[k] == '\\':
		return k
	}
	_, n := utf8.DecodeRuneInString(s[k:])
	return k + n
}

// plainLines trims every line and drops empty ones.
func plainLines(text string) string {
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimFunc(ln, unicode.IsSpace); ln != "" {
			lines = append(lines, ln)
		}
	}
	return strings.Join(lines, "\n")
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
