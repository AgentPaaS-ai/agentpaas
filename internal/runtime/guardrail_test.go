package runtime

import "testing"

// fakeFilter is a test ResponseFilter with configurable stream-safety.
type fakeFilter struct {
	name       string
	streamSafe bool
}

func (f *fakeFilter) IsStreamSafe() bool { return f.streamSafe }
func (f *fakeFilter) FilterName() string  { return f.name }

func TestSelectGuardrail_EmptyFiltersBuffers(t *testing.T) {
	// No filters configured: no explicit stream-safe declaration, fail closed.
	if got := SelectGuardrail(nil); got != GuardrailBufferedRelease {
		t.Fatalf("empty filters must select buffered_release; got %v", got)
	}
	if got := SelectGuardrail([]ResponseFilter{}); got != GuardrailBufferedRelease {
		t.Fatalf("empty filter slice must select buffered_release; got %v", got)
	}
}

func TestSelectGuardrail_AllStreamSafe_Incremental(t *testing.T) {
	filters := []ResponseFilter{
		&fakeFilter{name: "pii", streamSafe: true},
		&fakeFilter{name: "regex", streamSafe: true},
	}
	if got := SelectGuardrail(filters); got != GuardrailIncrementalRelease {
		t.Fatalf("all stream-safe filters must select incremental_release; got %v", got)
	}
}

func TestSelectGuardrail_AnyNonStreamSafe_Buffers(t *testing.T) {
	filters := []ResponseFilter{
		&fakeFilter{name: "pii", streamSafe: true},
		&fakeFilter{name: "strict_whole_response", streamSafe: false},
		&fakeFilter{name: "regex", streamSafe: true},
	}
	if got := SelectGuardrail(filters); got != GuardrailBufferedRelease {
		t.Fatalf("any non-stream-safe filter must select buffered_release; got %v", got)
	}
}

func TestSelectGuardrail_SingleNonStreamSafe_Buffers(t *testing.T) {
	filters := []ResponseFilter{
		&fakeFilter{name: "strict_whole_response", streamSafe: false},
	}
	if got := SelectGuardrail(filters); got != GuardrailBufferedRelease {
		t.Fatalf("single non-stream-safe filter must select buffered_release; got %v", got)
	}
}

func TestSelectGuardrail_SingleStreamSafe_Incremental(t *testing.T) {
	filters := []ResponseFilter{
		&fakeFilter{name: "pii", streamSafe: true},
	}
	if got := SelectGuardrail(filters); got != GuardrailIncrementalRelease {
		t.Fatalf("single stream-safe filter must select incremental_release; got %v", got)
	}
}

func TestSelectGuardrail_NilFilter_Buffers(t *testing.T) {
	// A nil filter in the slice cannot declare stream-safe semantics; fail closed.
	filters := []ResponseFilter{
		&fakeFilter{name: "pii", streamSafe: true},
		nil,
		&fakeFilter{name: "regex", streamSafe: true},
	}
	if got := SelectGuardrail(filters); got != GuardrailBufferedRelease {
		t.Fatalf("nil filter must select buffered_release; got %v", got)
	}
}
