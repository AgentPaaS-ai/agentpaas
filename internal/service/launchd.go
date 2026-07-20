package service

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// defaultLaunchdLabel is the standard launchd job label for agentpaasd.
const defaultLaunchdLabel = "com.agentpaas.daemon"

// LaunchdPlistConfig holds the inputs for generating a launchd plist.
type LaunchdPlistConfig struct {
	// Label is the launchd job label. If empty, defaults to defaultLaunchdLabel.
	Label string

	// DaemonPath is the absolute path to the agentpaasd binary.
	DaemonPath string

	// HomeDir is the agentpaas home directory, passed via --home.
	HomeDir string

	// EnvHome, if non-empty, is the AGENTPAAS_HOME environment variable value.
	// When set, it is included in the EnvironmentVariables dict in the plist.
	EnvHome string
}

// plistXML is the outer XML structure for a plist document.
type plistXML struct {
	XMLName xml.Name `xml:"plist"`
	Version string   `xml:"version,attr"`
	Dict    plistDictXML
}

// plistDictXML is a <dict> element containing key-value pairs.
type plistDictXML struct {
	XMLName xml.Name `xml:"dict"`
	Entries []plistEntryXML
}

// plistEntryXML is a single <key>/<value> pair within a dict.
type plistEntryXML struct {
	Key   string
	Value plistValueXML
}

// plistValueXML holds the value part of a plist entry (string, bool, array, or sub-dict).
type plistValueXML struct {
	String *string
	Bool   *bool
	Array  []string
	Dict   *plistDictXML
}

// MarshalXML implements xml.Marshaler for plistEntryXML.
// It emits <key>K</key><VALUE>...</VALUE> on a single line.
func (e plistEntryXML) MarshalXML(enc *xml.Encoder, _ xml.StartElement) error { // intentionally ignored (reviewed)
	// Emit <key>...</key>
	if err := enc.EncodeElement(e.Key, xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
		return err
	}
	// Emit the value element.
	return e.Value.marshal(enc)
}

// marshal emits the right plist element for the contained value type.
func (v plistValueXML) marshal(enc *xml.Encoder) error {
	switch {
	case v.String != nil:
		return enc.EncodeElement(*v.String, xml.StartElement{Name: xml.Name{Local: "string"}})
	case v.Bool != nil:
		// Encode as a regular element; we'll post-process to self-closing.
		return enc.EncodeElement("", xml.StartElement{Name: xml.Name{Local: boolName(*v.Bool)}})
	case v.Array != nil:
		arr := plistArrayXML{Items: v.Array}
		return enc.EncodeElement(arr, xml.StartElement{Name: xml.Name{Local: "array"}})
	case v.Dict != nil:
		return enc.EncodeElement(*v.Dict, xml.StartElement{Name: xml.Name{Local: "dict"}})
	}
	return nil
}

// boolName returns "true" or "false" for a plist bool.
func boolName(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// plistArrayXML is an <array> element containing string items.
type plistArrayXML struct {
	Items []string `xml:"string>value"`
}

// MarshalXML implements xml.Marshaler for plistArrayXML.
// It emits <array><string>...</string>...</array>.
func (a plistArrayXML) MarshalXML(enc *xml.Encoder, start xml.StartElement) error {
	if err := enc.EncodeToken(start); err != nil {
		return fmt.Errorf("plist array xml marshal xml: %w", err)
	}
	for _, item := range a.Items {
		if err := enc.EncodeElement(item, xml.StartElement{Name: xml.Name{Local: "string"}}); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}

// GenerateLaunchdPlist generates a macOS launchd plist XML document for agentpaasd.
//
// The output is deterministic: the same config always produces byte-identical XML.
// No timestamps or random values are included.
func GenerateLaunchdPlist(cfg LaunchdPlistConfig) ([]byte, error) {
	if cfg.HomeDir == "" {
		return nil, fmt.Errorf("service: HomeDir must not be empty")
	}

	label := cfg.Label
	if label == "" {
		label = defaultLaunchdLabel
	}

	// Build entries in a fixed order for determinism.
	var entries []plistEntryXML

	// Label
	t := true
	entries = append(entries, plistEntryXML{
		Key: "Label",
		Value: plistValueXML{
			String: &label,
		},
	})

	// ProgramArguments
	entries = append(entries, plistEntryXML{
		Key: "ProgramArguments",
		Value: plistValueXML{
			Array: []string{cfg.DaemonPath, "--home", cfg.HomeDir},
		},
	})

	// RunAtLoad
	entries = append(entries, plistEntryXML{
		Key: "RunAtLoad",
		Value: plistValueXML{
			Bool: &t,
		},
	})

	// KeepAlive
	entries = append(entries, plistEntryXML{
		Key: "KeepAlive",
		Value: plistValueXML{
			Bool: &t,
		},
	})

	// StandardOutPath
	outPath := cfg.HomeDir + "/logs/daemon.out.log"
	entries = append(entries, plistEntryXML{
		Key: "StandardOutPath",
		Value: plistValueXML{
			String: &outPath,
		},
	})

	// StandardErrorPath
	errPath := cfg.HomeDir + "/logs/daemon.err.log"
	entries = append(entries, plistEntryXML{
		Key: "StandardErrorPath",
		Value: plistValueXML{
			String: &errPath,
		},
	})

	// EnvironmentVariables (only if envHome is set)
	if cfg.EnvHome != "" {
		envDict := &plistDictXML{
			Entries: []plistEntryXML{
				{
					Key: "AGENTPAAS_HOME",
					Value: plistValueXML{
						String: &cfg.EnvHome,
					},
				},
			},
		}
		entries = append(entries, plistEntryXML{
			Key: "EnvironmentVariables",
			Value: plistValueXML{
				Dict: envDict,
			},
		})
	}

	doc := plistXML{
		Version: "1.0",
		Dict: plistDictXML{
			Entries: entries,
		},
	}

	// Marshal to XML with indentation.
	body, err := xml.MarshalIndent(doc, "", "\t")
	if err != nil {
		return nil, fmt.Errorf("service: marshal plist: %w", err)
	}

	// Prepend XML declaration and DOCTYPE.
	// xml.MarshalIndent does not include a DOCTYPE, so we add it manually.
	header := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n"

	result := append([]byte(header), body...)
	result = append(result, '\n')

	// Post-process to convert <true></true> to <true/> and <false></false> to <false/>.
	result = boolSelfClose(result)

	return result, nil
}

// boolSelfClose replaces <NAME></NAME> with <NAME/> for bool elements in a plist.
// This is deterministic and operates on byte output from xml.MarshalIndent.
func boolSelfClose(data []byte) []byte {
	s := string(data)
	s = strings.ReplaceAll(s, "<true></true>", "<true/>")
	s = strings.ReplaceAll(s, "<false></false>", "<false/>")
	return []byte(s)
}
