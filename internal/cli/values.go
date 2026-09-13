package cli

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strings"
)

// Size is a byte count parsed from the operator size syntax (for example 1GiB).
type Size int64

var sizeUnits = []struct {
	name string
	mult int64
}{
	{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
	{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1},
}

// ParseSize parses a non-negative size. Units B, KB, MB, GB, TB are powers of 1000; KiB, MiB, GiB,
// TiB are powers of 1024; a bare number is bytes. Units are case-insensitive and may be separated
// from the number by spaces. A decimal fraction is accepted with a multi-byte unit and the result
// is rounded down to whole bytes.
func ParseSize(s string) (Size, error) {
	size, err := parseSize(s)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return size, nil
}

func parseSize(s string) (Size, error) {
	text := strings.TrimSpace(s)
	i := 0
	for i < len(text) && (text[i] >= '0' && text[i] <= '9' || text[i] == '.') {
		i++
	}
	num, unit := text[:i], strings.TrimSpace(text[i:])
	if num == "" || strings.Count(num, ".") > 1 || num[0] == '.' || num[len(num)-1] == '.' {
		return 0, errors.New("want a number with an optional unit (B, KB, MB, GB, TB, KiB, MiB, GiB, TiB)")
	}
	mult := int64(-1)
	if unit == "" {
		mult = 1
	}
	for _, u := range sizeUnits {
		if strings.EqualFold(unit, u.name) {
			mult = u.mult
		}
	}
	if mult < 0 {
		return 0, fmt.Errorf("unknown unit %q (want B, KB, MB, GB, TB, KiB, MiB, GiB, TiB)", unit)
	}
	if mult == 1 && strings.Contains(num, ".") {
		return 0, errors.New("fractional bytes")
	}
	r, ok := new(big.Rat).SetString(num)
	if !ok {
		return 0, errors.New("malformed number")
	}
	r.Mul(r, new(big.Rat).SetInt64(mult))
	bytes := new(big.Int).Quo(r.Num(), r.Denom())
	if !bytes.IsInt64() {
		return 0, fmt.Errorf("exceeds %d bytes", int64(math.MaxInt64))
	}
	return Size(bytes.Int64()), nil
}

// String formats the size with the largest binary unit that divides it exactly.
func (s Size) String() string {
	for _, u := range sizeUnits[:4] {
		if s != 0 && int64(s)%u.mult == 0 {
			return fmt.Sprintf("%d%s", int64(s)/u.mult, u.name)
		}
	}
	return fmt.Sprintf("%dB", int64(s))
}

// Set implements flag.Value.
func (s *Size) Set(v string) error {
	parsed, err := parseSize(v)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// enumValue is a string flag restricted to a fixed set of values.
type enumValue struct {
	target  *string
	allowed []string
}

func (e *enumValue) String() string {
	if e == nil || e.target == nil {
		return ""
	}
	return *e.target
}

func (e *enumValue) Set(v string) error {
	for _, a := range e.allowed {
		if v == a {
			*e.target = v
			return nil
		}
	}
	return fmt.Errorf("must be one of %s", strings.Join(e.allowed, ", "))
}

// listValue is a repeatable string flag. From the environment, items are separated by the
// platform path list separator.
type listValue struct {
	target *[]string
}

func (l *listValue) String() string {
	if l == nil || l.target == nil {
		return ""
	}
	return strings.Join(*l.target, string(os.PathListSeparator))
}

func (l *listValue) Set(v string) error {
	if v == "" {
		return errors.New("must not be empty")
	}
	*l.target = append(*l.target, v)
	return nil
}

// reservedValue accepts any text for a flag of a later stage so parsing can report the specific
// "not available" error instead of an unknown-flag error.
type reservedValue struct {
	isBool bool
	set    bool
}

func (r *reservedValue) String() string   { return "" }
func (r *reservedValue) IsBoolFlag() bool { return r.isBool }
func (r *reservedValue) Set(string) error {
	r.set = true
	return nil
}
