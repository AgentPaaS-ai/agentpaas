package policy

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"gopkg.in/yaml.v3"
)

// validCredentialTypes is the set of allowed credential type values.
var validCredentialTypes = map[string]bool{
	"direct_lease": true,
	"header":       true,
	"brokered":     true,
	"file":         true,
	"oauth":        true,
}

// ParsePolicy reads a policy.yaml from r and returns the parsed Policy
// struct. It rejects unknown fields at every nesting level via strict
// YAML decoding, and validates credential type fields against the enum
// set at parse time (rejecting non-string scalars and invalid values).
// Schema version must be "1.0" or "1.1". Unknown versions are rejected.
func ParsePolicy(r io.Reader) (*Policy, error) {
	if r == nil {
		return nil, fmt.Errorf("policy: reader is nil")
	}
	// Catch typed-nil interfaces that bypass the `r == nil` check
	// (e.g. var r *bytes.Reader = nil; ParsePolicy(r)).
	if reflect.ValueOf(r).IsNil() {
		return nil, fmt.Errorf("policy: reader is nil (typed nil)")
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("policy: failed to read input: %w", err)
	}

	// Decode into a raw yaml.Node tree for type-level validation
	// before the struct loses type information through coercion.
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("policy: invalid yaml: %w", err)
	}

	if err := validateCredentialTypes(&doc); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}

	// Decode into the struct with strict known-fields checking.
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var p Policy
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("policy: invalid yaml: %w", err)
	}

	// Validate schema version. Empty version defaults to "1.0" for backward
	// compatibility with v0.2.3 policies that omit the version field.
	if p.Version == "" {
		p.Version = SchemaVersion10
	}
	if p.Version != SchemaVersion10 && p.Version != SchemaVersion11 {
		return nil, fmt.Errorf("policy: unknown schema version %q (must be %q or %q)",
			p.Version, SchemaVersion10, SchemaVersion11)
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

// validateCredentialTypes walks the raw YAML node tree and checks that
// every credential.type field is a string scalar with a valid enum value.
// Non-string scalars (int, bool, etc.) and invalid values are rejected.
func validateCredentialTypes(doc *yaml.Node) error {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "credentials" {
			seq := mapping.Content[i+1]
			if seq.Kind != yaml.SequenceNode {
				return fmt.Errorf("credentials must be a sequence")
			}
			for _, cred := range seq.Content {
				if err := validateCredentialEntry(cred); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateCredentialEntry checks a single credential mapping node for
// its "type" field.
func validateCredentialEntry(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "type" {
			typeNode := node.Content[i+1]
			if typeNode.Kind != yaml.ScalarNode || typeNode.Tag != "!!str" {
				return fmt.Errorf("credential.type must be a string, got YAML tag %s", typeNode.Tag)
			}
			if !validCredentialTypes[typeNode.Value] {
				return fmt.Errorf("invalid credential type %q: must be one of: header, brokered, file, direct_lease, oauth", typeNode.Value)
			}
		}
	}
	return nil
}
