package naming

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAgentRef(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantName  string
		wantPub8  string
		wantErr   bool
		errPrefix string // expected error message prefix (when wantErr=true)
	}{
		{
			name:     "bare valid name",
			input:    "weather",
			wantName: "weather",
			wantPub8: "",
			wantErr:  false,
		},
		{
			name:     "name@pub8 with valid pub8",
			input:    "weather@a1b2c3d4",
			wantName: "weather",
			wantPub8: "a1b2c3d4",
			wantErr:  false,
		},
		{
			name:     "name@pub8 uppercase normalized",
			input:    "weather@A1B2C3D4",
			wantName: "weather",
			wantPub8: "a1b2c3d4",
			wantErr:  false,
		},
		{
			name:     "name@pub8 mixed case normalized",
			input:    "weather@Ab2Cd4Ef",
			wantName: "weather",
			wantPub8: "ab2cd4ef",
			wantErr:  false,
		},
		{
			name:      "pub8 not hex (xyz)",
			input:     "weather@xyzabcde",
			wantErr:   true,
			errPrefix: "invalid agent reference",
		},
		{
			name:      "pub8 not 8 chars",
			input:     "weather@a1b2",
			wantErr:   true,
			errPrefix: "invalid agent reference",
		},
		{
			name:      "multiple @ separators",
			input:     "we@th@er",
			wantErr:   true,
			errPrefix: "invalid agent reference: multiple @",
		},
		{
			name:      "@ at start (empty name)",
			input:     "@a1b2c3d4",
			wantErr:   true,
			errPrefix: "invalid agent reference: empty name",
		},
		{
			name:      "too many hex chars",
			input:     "weather@a1b2c3d4e5",
			wantErr:   true,
			errPrefix: "invalid agent reference",
		},
		{
			name:      "empty string",
			input:     "",
			wantErr:   true,
			errPrefix: "invalid agent reference: empty string",
		},
		{
			name:      "empty pub8 after @",
			input:     "weather@",
			wantErr:   true,
			errPrefix: "invalid agent reference: empty pub8",
		},
		{
			name:     "single character name",
			input:    "a",
			wantName: "a",
			wantPub8: "",
			wantErr:  false,
		},
		{
			name:     "name with hyphen in middle",
			input:    "weather-agent",
			wantName: "weather-agent",
			wantPub8: "",
			wantErr:  false,
		},
		{
			name:     "63-char name (max length)",
			input:    "a" + strings.Repeat("b", 61) + "c", // 1 + 61 + 1 = 63
			wantName: "a" + strings.Repeat("b", 61) + "c",
			wantPub8: "",
			wantErr:  false,
		},
		{
			name:      "64-char name too long",
			input:     "a" + strings.Repeat("b", 62) + "c", // 1 + 62 + 1 = 64
			wantErr:   true,
			errPrefix: "invalid agent reference: invalid agent name: must be 1-63",
		},
		{
			name:      "name with leading hyphen",
			input:     "-weather",
			wantErr:   true,
			errPrefix: "invalid agent reference: invalid agent name",
		},
		{
			name:      "name with trailing hyphen",
			input:     "weather-",
			wantErr:   true,
			errPrefix: "invalid agent reference: invalid agent name",
		},
		{
			name:      "uppercase name",
			input:     "Weather",
			wantErr:   true,
			errPrefix: "invalid agent reference: invalid agent name",
		},
		{
			name:      "name with space",
			input:     "weather agent",
			wantErr:   true,
			errPrefix: "invalid agent reference: invalid agent name",
		},
		{
			name:      "name with special chars",
			input:     "weather!agent",
			wantErr:   true,
			errPrefix: "invalid agent reference: invalid agent name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotPub8, err := ParseAgentRef(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAgentRef(%q) expected error, got (name=%q, pub8=%q, nil)",
						tt.input, gotName, gotPub8)
				}
				if tt.errPrefix != "" && !strings.HasPrefix(err.Error(), tt.errPrefix) {
					t.Errorf("ParseAgentRef(%q) error = %v, want prefix %q",
						tt.input, err, tt.errPrefix)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseAgentRef(%q) unexpected error: %v", tt.input, err)
			}
			if gotName != tt.wantName {
				t.Errorf("ParseAgentRef(%q) name = %q, want %q", tt.input, gotName, tt.wantName)
			}
			if gotPub8 != tt.wantPub8 {
				t.Errorf("ParseAgentRef(%q) pub8 = %q, want %q", tt.input, gotPub8, tt.wantPub8)
			}
		})
	}
}

func TestFormatAgentRef(t *testing.T) {
	tests := []struct {
		name        string
		agentName   string
		fingerprint string
		want        string
	}{
		{
			name:        "fingerprint produces @pub8",
			agentName:   "weather",
			fingerprint: "a1b2c3d4e5f67890abcdef1234567890abcdef1234567890abcdef1234567890",
			want:        "weather@a1b2c3d4",
		},
		{
			name:        "fingerprint with uppercase normalizes",
			agentName:   "weather",
			fingerprint: "A1B2C3D4E5F67890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890",
			want:        "weather@a1b2c3d4",
		},
		{
			name:        "short fingerprint",
			agentName:   "weather",
			fingerprint: "a1b2c3d4",
			want:        "weather@a1b2c3d4",
		},
		{
			name:        "empty fingerprint returns bare name",
			agentName:   "weather",
			fingerprint: "",
			want:        "weather",
		},
		{
			name:        "name with hyphen and fingerprint",
			agentName:   "weather-agent",
			fingerprint: "deadbeef00000000000000000000000000000000000000000000000000000000",
			want:        "weather-agent@deadbeef",
		},
		{
			name:        "single char name with fingerprint",
			agentName:   "a",
			fingerprint: "ffffffff00000000000000000000000000000000000000000000000000000000",
			want:        "a@ffffffff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAgentRef(tt.agentName, tt.fingerprint)
			if got != tt.want {
				t.Errorf("FormatAgentRef(%q, %q) = %q, want %q",
					tt.agentName, tt.fingerprint, got, tt.want)
			}
		})
	}
}

func TestFormatAgentRef_RoundTrip(t *testing.T) {
	// FormatAgentRef output should be parseable by ParseAgentRef
	name := "my-agent"
	fp := "a1b2c3d4e5f67890abcdef1234567890abcdef1234567890abcdef1234567890"

	formatted := FormatAgentRef(name, fp)
	parsedName, parsedPub8, err := ParseAgentRef(formatted)
	if err != nil {
		t.Fatalf("round-trip ParseAgentRef(%q) failed: %v", formatted, err)
	}
	if parsedName != name {
		t.Errorf("round-trip name = %q, want %q", parsedName, name)
	}
	if parsedPub8 != fp[:8] {
		t.Errorf("round-trip pub8 = %q, want %q", parsedPub8, fp[:8])
	}
}

func TestFormatAgentRef_RoundTripBare(t *testing.T) {
	// Bare name with empty fingerprint should round-trip
	name := "weather"
	formatted := FormatAgentRef(name, "")
	if formatted != name {
		t.Fatalf("FormatAgentRef(%q, \"\") = %q, want %q", name, formatted, name)
	}
	parsedName, parsedPub8, err := ParseAgentRef(formatted)
	if err != nil {
		t.Fatalf("round-trip ParseAgentRef(%q) failed: %v", formatted, err)
	}
	if parsedName != name {
		t.Errorf("round-trip name = %q, want %q", parsedName, name)
	}
	if parsedPub8 != "" {
		t.Errorf("round-trip pub8 = %q, want empty", parsedPub8)
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "weather", false},
		{"valid single char", "a", false},
		{"valid with digit", "agent2", false},
		{"valid with hyphen", "weather-agent", false},
		{"valid max length (63)", "a" + strings.Repeat("b", 61) + "c", false},
		{"valid all digits", "123", false},
		{"valid numeric-start", "1agent", false},
		{"invalid: empty", "", true},
		{"invalid: 64 chars", "a" + strings.Repeat("b", 62) + "c", true},
		{"invalid: uppercase", "Weather", true},
		{"invalid: leading hyphen", "-weather", true},
		{"invalid: trailing hyphen", "weather-", true},
		{"invalid: double hyphen", "weather--agent", false}, // regex allows this
		{"invalid: underscore", "weather_agent", true},
		{"invalid: space", "weather agent", true},
		{"invalid: special char", "weather!agent", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateName(%q) expected error, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateName(%q) unexpected error: %v", tt.input, err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidName) {
				t.Errorf("ValidateName(%q) error = %v, want ErrInvalidName", tt.input, err)
			}
		})
	}
}

func TestValidatePub8(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid all digits", "00000000", false},
		{"valid all hex letters", "abcdef01", false},
		{"valid mixed", "a1b2c3d4", false},
		{"invalid: too short", "a1b2", true},
		{"invalid: too long", "a1b2c3d4e5", true},
		{"invalid: empty", "", true},
		{"invalid: non-hex char", "a1b2c3g4", true},
		{"invalid: uppercase", "A1B2C3D4", true}, // ValidatePub8 expects lowercase
		{"valid: zero prefix", "0000abcd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePub8(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("ValidatePub8(%q) expected error, got nil", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePub8(%q) unexpected error: %v", tt.input, err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidPub8) {
				t.Errorf("ValidatePub8(%q) error = %v, want ErrInvalidPub8", tt.input, err)
			}
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors are distinct and have messages
	if ErrInvalidAgentRef.Error() == "" {
		t.Error("ErrInvalidAgentRef has empty message")
	}
	if ErrInvalidName.Error() == "" {
		t.Error("ErrInvalidName has empty message")
	}
	if ErrInvalidPub8.Error() == "" {
		t.Error("ErrInvalidPub8 has empty message")
	}
	// Verify they are distinct
	if ErrInvalidAgentRef == ErrInvalidName {
		t.Error("ErrInvalidAgentRef and ErrInvalidName should be distinct")
	}
	if ErrInvalidName == ErrInvalidPub8 {
		t.Error("ErrInvalidName and ErrInvalidPub8 should be distinct")
	}
}