//go:build unix

package report

// Linux NAME_MAX and PATH_MAX. Other Unix builds use the same conservative limits.
const (
	defaultNameMax = 255
	defaultPathMax = 4096
)

func pathUnitLen(s string) int { return len(s) }
