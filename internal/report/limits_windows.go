//go:build windows

package report

import "unicode/utf16"

// Windows MAX_PATH is 260 including the NUL terminator, without long-path prefix.
const (
	defaultNameMax = 255
	defaultPathMax = 259
)

func pathUnitLen(s string) int { return len(utf16.Encode([]rune(s))) }
