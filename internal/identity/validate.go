package identity

import (
	"errors"
	"fmt"
	"strings"
)

const maxAgentNameLength = 253

// ValidateAgentName validates an agent name for safe use in identity and
// filesystem-adjacent contexts.
func ValidateAgentName(name string) error {
	if name == "" {
		return errors.New("agent name must not be empty")
	}
	if len(name) > maxAgentNameLength {
		return fmt.Errorf("agent name must not exceed %d characters", maxAgentNameLength)
	}
	if strings.Contains(name, "\x00") {
		return errors.New("agent name must not contain null bytes")
	}
	if strings.Contains(name, "..") {
		return errors.New("agent name must not contain path traversal")
	}
	if strings.ContainsAny(name, `/\`) {
		return errors.New("agent name must not contain path separators")
	}
	return nil
}
