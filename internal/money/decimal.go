// Package money provides an exact decimal type for USD amounts with
// nano-USD precision (9 decimal places). It stores values internally as
// int64 nano-USD with checked arithmetic and does not use float64 anywhere.
//
// B32 and B34 reuse this package for cost calculations.
package money

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Decimal represents a USD amount with nano-USD precision (9 decimal places).
// Stored internally as int64 nano-USD.
type Decimal struct {
	nano int64
}

// NanoUSD returns the internal nano-USD representation.
// Exported for use by B32/B34 cost calculations.
func (d Decimal) NanoUSD() int64 { return d.nano }

// Parse parses a string representation of a USD amount into a Decimal.
// Rules:
//   - Reject empty string
//   - Reject exponent notation (e.g. "1e5", "1E5", "1.5e2")
//   - Reject negative values
//   - Reject NaN, Inf (any case)
//   - Reject excess precision (>9 decimal places)
//   - Reject overflow (>2^63-1 nano-USD)
//   - Accept leading zero before decimal point (e.g. "0.5")
//   - No leading/trailing whitespace
func Parse(s string) (*Decimal, error) {
	if s == "" {
		return nil, fmt.Errorf("money: empty string")
	}

	// Check for whitespace
	for _, r := range s {
		if r <= 0x20 && (r == ' ' || r == '	' || r == '\n' || r == '\r') {
			return nil, fmt.Errorf("money: whitespace not allowed in %q", s)
		}
	}

	// Reject exponent notation and NaN/Inf (case-insensitive)
	lower := strings.ToLower(s)
	if strings.ContainsAny(lower, "en") {
		// Check 'e' for exponent notation (must not be part of a word)
		if strings.ContainsRune(lower, 'e') {
			return nil, fmt.Errorf("money: exponent notation not allowed: %q", s)
		}
		// Check 'n' for NaN/Inf
		switch lower {
		case "nan", "inf", "+inf", "-inf", "infinity", "+infinity", "-infinity":
			return nil, fmt.Errorf("money: NaN/Inf not allowed: %q", s)
		}
	}

	// Reject negative
	if strings.HasPrefix(s, "-") {
		return nil, fmt.Errorf("money: negative values not allowed: %q", s)
	}

	// Split into integer and fractional parts
	parts := strings.SplitN(s, ".", 2)
	if len(parts) > 2 {
		return nil, fmt.Errorf("money: multiple decimal points: %q", s)
	}

	intPart := parts[0]
	if intPart == "" {
		return nil, fmt.Errorf("money: missing integer part: %q", s)
	}

	// Validate integer part is numeric
	if _, err := strconv.ParseUint(intPart, 10, 64); err != nil {
		return nil, fmt.Errorf("money: invalid integer part: %q", s)
	}

	var fracPart string
	if len(parts) == 2 {
		fracPart = parts[1]
		if fracPart == "" {
			return nil, fmt.Errorf("money: trailing decimal point: %q", s)
		}
	}

	// Check precision
	if len(fracPart) > 9 {
		return nil, fmt.Errorf("money: excess precision (%d decimal places, max 9): %q", len(fracPart), s)
	}

	// Pad or trim fractional part to 9 digits
	fracPadded := fracPart + strings.Repeat("0", 9-len(fracPart))

	// Parse into big.Int to check overflow
	intBI := new(big.Int)
	if _, ok := intBI.SetString(intPart, 10); !ok {
		return nil, fmt.Errorf("money: invalid integer part: %q", s)
	}

	fracBI := new(big.Int)
	if _, ok := fracBI.SetString(fracPadded, 10); !ok {
		return nil, fmt.Errorf("money: invalid fractional part: %q", s)
	}

	// total = intPart * 1e9 + fracPart
	mul := big.NewInt(1_000_000_000)
	total := new(big.Int).Mul(intBI, mul)
	total.Add(total, fracBI)

	if total.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return nil, fmt.Errorf("money: overflow: %q exceeds max nano-USD", s)
	}

	nano := total.Int64()
	if nano < 0 {
		return nil, fmt.Errorf("money: overflow: %q exceeds max nano-USD", s)
	}

	return &Decimal{nano: nano}, nil
}

// MustParse parses a string or panics. Useful for test helpers.
func MustParse(s string) *Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// String returns the canonical string representation.
// No trailing zeros except keep at least one digit before decimal point.
func (d *Decimal) String() string {
	if d.nano == 0 {
		return "0"
	}

	intPart := d.nano / 1_000_000_000
	fracPart := d.nano % 1_000_000_000

	if fracPart == 0 {
		return strconv.FormatInt(intPart, 10)
	}

	// Format fractional part: left-pad with zeros to 9 digits, then trim trailing zeros
	fracStr := strconv.FormatInt(fracPart, 10)
	// Left-pad with zeros to 9 digits
	for len(fracStr) < 9 {
		fracStr = "0" + fracStr
	}
	// Trim trailing zeros
	fracStr = strings.TrimRight(fracStr, "0")

	return fmt.Sprintf("%d.%s", intPart, fracStr)
}

// IsZero returns true if the value is zero.
func (d *Decimal) IsZero() bool {
	return d.nano == 0
}

// Cmp compares two Decimals. Returns -1 if d < other, 0 if equal, 1 if d > other.
func (d *Decimal) Cmp(other *Decimal) int {
	switch {
	case d.nano < other.nano:
		return -1
	case d.nano > other.nano:
		return 1
	default:
		return 0
	}
}

// Add returns a new Decimal that is the sum of d and other.
// Returns an error on overflow.
func (d *Decimal) Add(other *Decimal) (*Decimal, error) {
	result := d.nano + other.nano
	// Overflow detection: if both operands are positive but the result is
	// negative, the sum wrapped around int64's positive range.
	if d.nano > 0 && other.nano > 0 && result < 0 {
		return nil, fmt.Errorf("money: overflow adding %s + %s", d.String(), other.String())
	}
	return &Decimal{nano: result}, nil
}

// Sub returns a new Decimal that is the difference of d and other.
// Returns an error on underflow (negative result).
func (d *Decimal) Sub(other *Decimal) (*Decimal, error) {
	if d.nano < other.nano {
		return nil, fmt.Errorf("money: underflow subtracting %s - %s", d.String(), other.String())
	}
	return &Decimal{nano: d.nano - other.nano}, nil
}

// FromNanoUSD creates a Decimal from a nano-USD value.
func FromNanoUSD(nano int64) *Decimal {
	return &Decimal{nano: nano}
}