package strutil

import (
	"bytes"
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single_no_nl", "hello", []string{"hello"}},
		{"single_with_nl", "hello\n", []string{"hello"}},
		{"two_lines", "a\nb", []string{"a", "b"}},
		{"two_with_trailing_nl", "a\nb\n", []string{"a", "b"}},
		{"blank_line", "a\n\nb", []string{"a", "", "b"}},
		{"only_nl", "\n", []string{""}},
		{"crlf_keeps_cr", "a\r\nb", []string{"a\r", "b"}},
		{"unicode", "日本語\n中文", []string{"日本語", "中文"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitLines(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SplitLines(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSplitCompleteLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want [][]byte
	}{
		{"empty", nil, nil},
		{"no_complete", []byte("partial"), nil},
		{"one_complete", []byte("a\n"), [][]byte{[]byte("a")}},
		{"two_complete_trailing_partial", []byte("a\nb\npartial"), [][]byte{[]byte("a"), []byte("b")}},
		{"blank_complete", []byte("\n"), [][]byte{[]byte("")}},
		{"crlf", []byte("a\r\nb\n"), [][]byte{[]byte("a\r"), []byte("b")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitCompleteLines(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d; got=%q", len(got), len(tc.want), got)
			}
			for i := range got {
				if !bytes.Equal(got[i], tc.want[i]) {
					t.Fatalf("[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSplitCompleteLines_DoesNotAdvancePastIncomplete(t *testing.T) {
	t.Parallel()
	// Callers use start offset = sum of complete line lengths + newlines.
	data := []byte("line1\nline2\nincompl")
	lines := SplitCompleteLines(data)
	if len(lines) != 2 {
		t.Fatalf("want 2 complete lines, got %d", len(lines))
	}
	consumed := 0
	for _, ln := range lines {
		consumed += len(ln) + 1 // + newline
	}
	rest := data[consumed:]
	if string(rest) != "incompl" {
		t.Fatalf("remainder = %q, want %q", rest, "incompl")
	}
}
