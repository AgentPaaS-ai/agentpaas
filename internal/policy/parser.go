package policy

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ParsePolicy reads a policy.yaml from r and returns the parsed Policy
// struct. It rejects unknown fields at every nesting level via strict
// YAML decoding.
func ParsePolicy(r io.Reader) (*Policy, error) {
	if r == nil {
		return nil, fmt.Errorf("policy: reader is nil")
	}

	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var p Policy
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("policy: invalid yaml: %w", err)
	}

	// Decode a second time to ensure there is no trailing document.
	// yaml.v3 Decode does not return io.EOF reliably on single-document
	// streams, so we check explicitly.
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("policy: expected exactly one document, found multiple")
	}

	return &p, nil
}

// MustParse parses the policy or panics. Useful for test helpers.
func MustParse(r io.Reader) *Policy {
	p, err := ParsePolicy(r)
	if err != nil {
		panic(err)
	}
	return p
}