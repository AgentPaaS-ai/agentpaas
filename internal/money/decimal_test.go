package money

import (
	"math"
	"testing"
)

func TestParse_ValidValues(t *testing.T) {
	tests := []struct {
		input string
		want  string // canonical string
		nano  int64
	}{
		{"0", "0", 0},
		{"0.0", "0", 0},
		{"0.000000000", "0", 0},
		{"1", "1", 1_000_000_000},
		{"1.0", "1", 1_000_000_000},
		{"1.000000000", "1", 1_000_000_000},
		{"0.5", "0.5", 500_000_000},
		{"0.500000000", "0.5", 500_000_000},
		{"0.000000001", "0.000000001", 1},
		{"99.999999999", "99.999999999", 99_999_999_999},
		{"0.000000009", "0.000000009", 9},
		{"10.000000000", "10", 10_000_000_000},
		{"100.000000001", "100.000000001", 100_000_000_001},
		{"9223372036", "9223372036", math.MaxInt64 - math.MaxInt64%1_000_000_000},    // max int USD
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			d, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.input, err)
			}
			if d.nano != tc.nano {
				t.Errorf("Parse(%q).nano = %d, want %d", tc.input, d.nano, tc.nano)
			}
			got := d.String()
			if got != tc.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParse_RejectInvalid(t *testing.T) {
	invalid := []string{
		"",           // empty
		"-1",         // negative
		"-0.5",       // negative fractional
		"1e5",        // exponent notation
		"1E5",        // uppercase exponent
		"1.5e2",      // fractional exponent
		"NaN",        // not a number
		"Inf",        // infinity
		"+Inf",       // positive infinity
		"-Inf",       // negative infinity
		"inf",        // lowercase infinity
		"nan",        // lowercase nan
		".5",         // leading dot
		"1.",         // trailing dot
		"0.0000000001",    // 10 decimal places (excess precision)
		"0.00000000001",   // 11 decimal places
		"0.1234567890",    // 10 decimal places
		"abc",        // not a number
		"1.2.3",      // multiple dots
		" 1",         // leading space
		"1 ",         // trailing space
		"\t1",        // leading tab
		"1\n",        // trailing newline
	}
	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			_, err := Parse(input)
			if err == nil {
				t.Fatalf("Parse(%q) expected error, got nil", input)
			}
		})
	}
}

func TestParse_Overflow(t *testing.T) {
	// 2^63 nano-USD = 9223372036.854775808 USD (approx)
	// math.MaxInt64 = 9223372036854775807 nano-USD
	// 9223372036.854775808 USD exactly is MaxInt64 nano-USD
	overflow := []string{
		"9223372036.854775808", // MaxInt64 + 1 nano (exceeds)
		"10000000000",          // 10B USD = 10^19 nano-USD > MaxInt64
		"9999999999999999999",  // clearly > MaxInt64
	}
	for _, input := range overflow {
		t.Run(input, func(t *testing.T) {
			_, err := Parse(input)
			if err == nil {
				t.Fatalf("Parse(%q) expected overflow error, got nil", input)
			}
		})
	}
}

func TestIsZero(t *testing.T) {
	zero, _ := Parse("0")
	if !zero.IsZero() {
		t.Error("0 should be zero")
	}
	nonZero, _ := Parse("1")
	if nonZero.IsZero() {
		t.Error("1 should not be zero")
	}
	small, _ := Parse("0.000000001")
	if small.IsZero() {
		t.Error("0.000000001 should not be zero")
	}
}

func TestCmp(t *testing.T) {
	a, _ := Parse("1.5")
	b, _ := Parse("1.5")
	c, _ := Parse("2.0")
	zero, _ := Parse("0")

	if a.Cmp(b) != 0 {
		t.Error("1.5 vs 1.5 should be equal")
	}
	if a.Cmp(c) >= 0 {
		t.Error("1.5 vs 2.0 should be less")
	}
	if c.Cmp(a) <= 0 {
		t.Error("2.0 vs 1.5 should be greater")
	}
	if a.Cmp(zero) <= 0 {
		t.Error("1.5 vs 0 should be greater")
	}
	if zero.Cmp(a) >= 0 {
		t.Error("0 vs 1.5 should be less")
	}
}

func TestAdd(t *testing.T) {
	tests := []struct {
		a, b, sum string
	}{
		{"0", "0", "0"},
		{"1", "2", "3"},
		{"0.5", "0.5", "1"},
		{"0.000000001", "0.000000001", "0.000000002"},
		{"1.000000001", "2.000000002", "3.000000003"},
		{"99.999999999", "0.000000001", "100"},
	}
	for _, tc := range tests {
		t.Run(tc.a+"+"+tc.b, func(t *testing.T) {
			da, _ := Parse(tc.a)
			db, _ := Parse(tc.b)
			sum, err := da.Add(db)
			if err != nil {
				t.Fatalf("Add(%q, %q): %v", tc.a, tc.b, err)
			}
			if sum.String() != tc.sum {
				t.Errorf("Add(%q, %q) = %q, want %q", tc.a, tc.b, sum.String(), tc.sum)
			}
		})
	}
}

func TestAdd_Overflow(t *testing.T) {
	maxVal, _ := Parse("9223372036.854775807") // math.MaxInt64 nano-USD
	one, _ := Parse("0.000000001")
	_, err := maxVal.Add(one)
	if err == nil {
		t.Fatal("expected overflow error for max+1nano")
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		a, b, diff string
	}{
		{"0", "0", "0"},
		{"5", "3", "2"},
		{"1", "0.5", "0.5"},
		{"0.000000002", "0.000000001", "0.000000001"},
		{"100", "0.000000001", "99.999999999"},
	}
	for _, tc := range tests {
		t.Run(tc.a+"-"+tc.b, func(t *testing.T) {
			da, _ := Parse(tc.a)
			db, _ := Parse(tc.b)
			diff, err := da.Sub(db)
			if err != nil {
				t.Fatalf("Sub(%q, %q): %v", tc.a, tc.b, err)
			}
			if diff.String() != tc.diff {
				t.Errorf("Sub(%q, %q) = %q, want %q", tc.a, tc.b, diff.String(), tc.diff)
			}
		})
	}
}

func TestSub_Underflow(t *testing.T) {
	a, _ := Parse("0.5")
	b, _ := Parse("1.0")
	_, err := a.Sub(b)
	if err == nil {
		t.Fatal("expected underflow error for 0.5 - 1.0")
	}

	zero, _ := Parse("0")
	one, _ := Parse("1")
	_, err = zero.Sub(one)
	if err == nil {
		t.Fatal("expected underflow error for 0 - 1")
	}
}

func TestSub_Zero(t *testing.T) {
	a, _ := Parse("5")
	b, _ := Parse("5")
	diff, err := a.Sub(b)
	if err != nil {
		t.Fatalf("Sub(5,5): %v", err)
	}
	if !diff.IsZero() {
		t.Errorf("Sub(5,5) = %q, want 0", diff.String())
	}
}

func TestString_Canonical(t *testing.T) {
	tests := []struct {
		nano int64
		want string
	}{
		{0, "0"},
		{1, "0.000000001"},
		{9, "0.000000009"},
		{10, "0.00000001"},
		{99, "0.000000099"},
		{100, "0.0000001"},
		{999, "0.000000999"},
		{1000, "0.000001"},
		{1_000_000, "0.001"},
		{1_000_000_000, "1"},
		{1_500_000_000, "1.5"},
		{1_000_000_001, "1.000000001"},
		{5_000_000_000, "5"},
		{10_000_000_000, "10"},
		{99_999_999_999, "99.999999999"},
		{100_000_000_000, "100"},
		{math.MaxInt64, "9223372036.854775807"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			d := Decimal{nano: tc.nano}
			got := d.String()
			if got != tc.want {
				t.Errorf("String(%d) = %q, want %q", tc.nano, got, tc.want)
			}
		})
	}
}

func TestParseAndStringRoundTrip(t *testing.T) {
	values := []string{
		"0",
		"0.000000001",
		"0.5",
		"1",
		"1.5",
		"99.999999999",
		"100",
		"9223372036.854775807",
		"0.00000001",
		"0.0000001",
		"0.001",
		"10.000000001",
	}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			d, err := Parse(v)
			if err != nil {
				t.Fatalf("Parse(%q): %v", v, err)
			}
			got := d.String()
			if got != v {
				t.Errorf("round-trip: Parse(%q).String() = %q", v, got)
			}
		})
	}
}

func TestMustParse(t *testing.T) {
	d := MustParse("1.5")
	if d.String() != "1.5" {
		t.Errorf("MustParse(1.5) = %q, want 1.5", d.String())
	}
}

func TestMustParse_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustParse should panic on invalid input")
		}
	}()
	MustParse("not-a-number")
}

func TestFromNanoUSD(t *testing.T) {
	t.Parallel()
	cases := []struct {
		nano int64
		want string
	}{
		{0, "0"},
		{1, "0.000000001"},
		{1_000_000_000, "1"},
		{1_500_000_000, "1.5"},
		{-1, ""}, // negative nano is constructible but not via Parse
	}
	for _, tc := range cases {
		d := FromNanoUSD(tc.nano)
		if d == nil {
			t.Fatalf("FromNanoUSD(%d) returned nil", tc.nano)
		}
		if d.NanoUSD() != tc.nano {
			t.Errorf("NanoUSD() = %d, want %d", d.NanoUSD(), tc.nano)
		}
		if tc.want != "" && d.String() != tc.want {
			t.Errorf("String() = %q, want %q", d.String(), tc.want)
		}
	}
	// Round-trip: Parse → NanoUSD → FromNanoUSD → String
	orig, err := Parse("42.125")
	if err != nil {
		t.Fatal(err)
	}
	rt := FromNanoUSD(orig.NanoUSD())
	if rt.String() != orig.String() {
		t.Errorf("round-trip %q -> nano -> %q", orig.String(), rt.String())
	}
}

func TestParse_EdgePlusPrefix(t *testing.T) {
	t.Parallel()
	// Leading '+' is not documented as allowed — must reject.
	if _, err := Parse("+1"); err == nil {
		t.Fatal(`Parse("+1") should reject leading plus`)
	}
	// Unicode digits must not be accepted.
	if _, err := Parse("١"); err == nil {
		t.Fatal("Parse arabic-indic digit should reject")
	}
}