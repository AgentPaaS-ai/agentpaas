package mcpmanager

import "strings"

// HostAffectingCapability classifies tools that can affect the host system.
type HostAffectingCapability string

const (
	// CapabilityNone means the tool has no host-affecting capability.
	CapabilityNone HostAffectingCapability = "none"
	// CapabilityHostAffecting means the tool can control the host
	// (browser, shell, filesystem, AppleScript, desktop automation).
	CapabilityHostAffecting HostAffectingCapability = "host_affecting"
)

// hostAffectingToolPatterns matches tool names that are host-affecting.
// These require explicit confirmation before enabling.
var hostAffectingToolPatterns = []string{
	"shell", "exec", "bash", "terminal",
	"browser", "selenium", "playwright", "puppeteer",
	"filesystem", "write_file", "edit_file", "delete_file", "fs",
	"applescript", "osascript", "automator",
	"desktop", "mouse", "keyboard", "screen", "cua",
}

// ClassifyTool returns the host-affecting capability for a tool name.
// A tool is host-affecting if its name contains any of the
// hostAffectingToolPatterns substrings (case-insensitive).
func ClassifyTool(toolName string) HostAffectingCapability {
	lower := strings.ToLower(toolName)
	for _, pattern := range hostAffectingToolPatterns {
		if strings.Contains(lower, pattern) {
			return CapabilityHostAffecting
		}
	}
	return CapabilityNone
}

// IsHostAffecting returns true if the tool name matches a host-affecting pattern.
func IsHostAffecting(toolName string) bool {
	return ClassifyTool(toolName) == CapabilityHostAffecting
}
