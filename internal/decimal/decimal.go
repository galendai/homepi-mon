// Package decimal provides an exact base-10 numeric type for money and quota
// values.
//
// HL-Spec 7 forbids accumulating billing amounts in binary floating point.
// Phase 1 never sums provider amounts, it only parses, compares and renders
// them, so a fixed-point (unscaled int64 + scale) representation is enough and
// round-trips the upstream text exactly.
package decimal

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// MaxScale bounds the number of fractional digits we accept, keeping the
// unscaled value inside int64 for every realistic balance.
const MaxScale = 9

var errSyntax = errors.New("decimal: invalid syntax")

// Decimal is an exact base-10 value: unscaled * 10^-scale.
// The zero value is a valid 0.
type Decimal struct {
	unscaled int64
	scale    int32
}

// New builds a Decimal from an unscaled value and scale.
func New(unscaled int64, scale int32) Decimal {
	return Decimal{unscaled: unscaled, scale: scale}
}

// Parse converts decimal text such as "8.20" or "-1234.5" into a Decimal.
// The scale of the input is preserved so String round-trips the original text.
func Parse(s string) (Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Decimal{}, errSyntax
	}
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return Decimal{}, errSyntax
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if hasFrac && fracPart == "" {
		return Decimal{}, errSyntax
	}
	if intPart == "" && !hasFrac {
		return Decimal{}, errSyntax
	}
	if len(fracPart) > MaxScale {
		return Decimal{}, fmt.Errorf("decimal: scale %d exceeds max %d", len(fracPart), MaxScale)
	}
	digits := intPart + fracPart
	if digits == "" {
		return Decimal{}, errSyntax
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return Decimal{}, errSyntax
		}
	}
	unscaled, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Decimal{}, fmt.Errorf("decimal: %q out of range", s)
	}
	if neg {
		unscaled = -unscaled
	}
	return Decimal{unscaled: unscaled, scale: int32(len(fracPart))}, nil
}

// MustParse is Parse for compile-time-known constants and test fixtures.
func MustParse(s string) Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// String renders the value with its original scale.
func (d Decimal) String() string {
	if d.scale == 0 {
		return strconv.FormatInt(d.unscaled, 10)
	}
	neg := d.unscaled < 0
	u := d.unscaled
	if neg {
		u = -u
	}
	digits := strconv.FormatInt(u, 10)
	for len(digits) <= int(d.scale) {
		digits = "0" + digits
	}
	cut := len(digits) - int(d.scale)
	out := digits[:cut] + "." + digits[cut:]
	if neg {
		out = "-" + out
	}
	return out
}

// Scale reports the number of fractional digits.
func (d Decimal) Scale() int32 { return d.scale }

// IsZero reports whether the value is exactly zero.
func (d Decimal) IsZero() bool { return d.unscaled == 0 }

// Rat returns the exact value as a big.Rat, for division that must not lose
// precision before rounding.
func (d Decimal) Rat() *big.Rat {
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d.scale)), nil)
	return new(big.Rat).SetFrac(big.NewInt(d.unscaled), den)
}

// Cmp returns -1, 0 or +1 comparing d with o, correct across differing scales.
func (d Decimal) Cmp(o Decimal) int { return d.Rat().Cmp(o.Rat()) }

// Rescale returns the value rendered with exactly n fractional digits,
// rounding half away from zero. It is display-only and never feeds arithmetic.
func (d Decimal) Rescale(n int32) string {
	if n < 0 {
		n = 0
	}
	r := d.Rat()
	return r.FloatString(int(n))
}

// MarshalJSON encodes the value as a JSON string to keep it exact on the wire.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(d.String())), nil
}

// UnmarshalJSON accepts either a JSON string ("8.20") or a JSON number (8.20).
// Numbers are read from their literal text, so no float rounding occurs.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*d = Decimal{}
		return nil
	}
	if len(s) >= 2 && s[0] == '"' {
		unquoted, err := strconv.Unquote(s)
		if err != nil {
			return errSyntax
		}
		s = unquoted
	}
	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// PercentOf returns round(d / limit * 100) as an integer percent, plus ok=false
// when the percentage is undefined (limit is zero or negative).
//
// MOD-001 7 forbids deriving a percentage when the upstream limit is unknown;
// callers must not substitute a guess for a missing limit.
func PercentOf(d, limit Decimal) (int, bool) {
	if limit.unscaled <= 0 {
		return 0, false
	}
	q := new(big.Rat).Quo(d.Rat(), limit.Rat())
	q.Mul(q, big.NewRat(100, 1))
	// FloatString rounds half away from zero, which matches the UI spec's
	// "round to the nearest cell" wording.
	i, err := strconv.Atoi(q.FloatString(0))
	if err != nil {
		return 0, false
	}
	return i, true
}
