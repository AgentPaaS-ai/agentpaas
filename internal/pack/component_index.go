package pack

import (
	"encoding/json"
	"strings"
)

// ComponentIndexSchemaV1 is the tenant component index schema identifier.
const ComponentIndexSchemaV1 = "component-index/1"

// SchemaNotDeclared is the honest-empty marker for omitted tool/route schemas.
const SchemaNotDeclared = "not declared"

// ComponentIndex is the pack-time, signed tenant component card.
type ComponentIndex struct {
	SchemaVersion string              `json:"schema_version"`
	Kind          string              `json:"kind"`
	Name          string              `json:"name"`
	Title         string              `json:"title,omitempty"`
	Version       string              `json:"version,omitempty"`
	Description   string              `json:"description,omitempty"`
	Delegates     []string            `json:"delegates,omitempty"`
	Egress        []string            `json:"egress"`
	Bindings      []ComponentBinding  `json:"bindings"`
	Provenance    ComponentProvenance `json:"provenance"`
	Invoke        ComponentInvoke     `json:"invoke"`
	MCP           *MCPIndex           `json:"mcp,omitempty"`
	Agent         *AgentIndex         `json:"agent,omitempty"`
	HTTP          *HTTPIndex          `json:"http,omitempty"`
	Sandbox       *SandboxIndex       `json:"sandbox,omitempty"`
}

// ComponentBinding is a label-only secret binding (never values).
type ComponentBinding struct {
	SecretName  string `json:"secret_name"`
	InjectAs    string `json:"inject_as,omitempty"`
	HostPattern string `json:"host_pattern,omitempty"`
}

// ComponentProvenance is the signed package identity block.
type ComponentProvenance struct {
	ImageDigest  string `json:"image_digest,omitempty"`
	SBOMDigest   string `json:"sbom_digest,omitempty"`
	PolicyDigest string `json:"policy_digest,omitempty"`
	Signature    string `json:"signature,omitempty"`
	Publisher    string `json:"publisher,omitempty"`
}

// ComponentInvoke is the invoke contract + auth mode.
type ComponentInvoke struct {
	Contract string `json:"contract,omitempty"`
	Auth     string `json:"auth"`
}

// MCPIndex is the MCP-aligned card. Unknown/newer fields live in Meta.
type MCPIndex struct {
	ProtocolVersion string                 `json:"protocolVersion,omitempty"`
	Capabilities    map[string]interface{} `json:"capabilities,omitempty"`
	Transport       *MCPTransport          `json:"transport,omitempty"`
	Tools           []MCPTool              `json:"tools"`
	Prompts         []MCPPrompt            `json:"prompts,omitempty"`
	Resources       []MCPResource          `json:"resources,omitempty"`
	Meta            map[string]interface{} `json:"_meta,omitempty"`
}

// MCPTransport is the declared MCP transport.
type MCPTransport struct {
	Type string `json:"type,omitempty"`
	URL  string `json:"url,omitempty"`
}

// MCPTool is one declared MCP tool. Schemas are objects or "not declared".
type MCPTool struct {
	Name         string      `json:"name"`
	Title        string      `json:"title,omitempty"`
	Description  string      `json:"description,omitempty"`
	InputSchema  interface{} `json:"inputSchema"`
	OutputSchema interface{} `json:"outputSchema"`
}

// MCPPrompt is one declared MCP prompt.
type MCPPrompt struct {
	Name        string              `json:"name"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Arguments   []MCPPromptArgument `json:"arguments,omitempty"`
}

// MCPPromptArgument is a prompt argument declaration.
type MCPPromptArgument struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// MCPResource is one declared MCP resource.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Description string `json:"description,omitempty"`
}

// AgentIndex is the agent card.
type AgentIndex struct {
	LLM    []AgentLLM  `json:"llm"`
	Invoke AgentInvoke `json:"invoke"`
}

// AgentLLM is one declared model surface (credential label only).
type AgentLLM struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	CredentialLabel string `json:"credential_label,omitempty"`
}

// AgentInvoke is the agent invoke contract.
type AgentInvoke struct {
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
}

// HTTPIndex is the HTTP tool card.
type HTTPIndex struct {
	Routes []HTTPRoute `json:"routes"`
}

// HTTPRoute is one declared HTTP route. Schemas are objects or "not declared".
type HTTPRoute struct {
	Method       string      `json:"method"`
	Path         string      `json:"path"`
	InputSchema  interface{} `json:"inputSchema,omitempty"`
	OutputSchema interface{} `json:"outputSchema,omitempty"`
}

// SandboxIndex is the sandbox card.
type SandboxIndex struct {
	Capabilities []string      `json:"capabilities"`
	Transport    *MCPTransport `json:"transport,omitempty"`
}

// ComponentIndexProvenance is pack-time digest/publisher input.
type ComponentIndexProvenance struct {
	ImageDigest  string
	SBOMDigest   string
	PolicyDigest string
	Signature    string
	Publisher    string
}

var knownMCPKeys = map[string]struct{}{
	"protocolVersion":  {},
	"protocol_version": {},
	"capabilities":     {},
	"transport":        {},
	"tools":            {},
	"prompts":          {},
	"resources":        {},
	"_meta":            {},
}

// BuildComponentIndex emits a full component-index/1 card from pack inputs.
// Undeclared sections are honest-empty. Unknown MCP fields pass through _meta.
func BuildComponentIndex(agent *AgentYAML, prov ComponentIndexProvenance) *ComponentIndex {
	idx := &ComponentIndex{
		SchemaVersion: ComponentIndexSchemaV1,
		Kind:          "agent",
		Egress:        []string{},
		Bindings:      []ComponentBinding{},
		Provenance: ComponentProvenance{
			ImageDigest:  strings.TrimSpace(prov.ImageDigest),
			SBOMDigest:   strings.TrimSpace(prov.SBOMDigest),
			PolicyDigest: strings.TrimSpace(prov.PolicyDigest),
			Signature:    strings.TrimSpace(prov.Signature),
			Publisher:    strings.TrimSpace(prov.Publisher),
		},
		Invoke: ComponentInvoke{Auth: "inv_token"},
	}
	if agent != nil {
		idx.Name = strings.TrimSpace(agent.Name)
		idx.Version = strings.TrimSpace(agent.Version)
		idx.Description = strings.TrimSpace(agent.Description)
		idx.Kind = mapComponentKind(agent.Kind)
		if len(agent.Egress) > 0 {
			idx.Egress = append([]string{}, agent.Egress...)
		}
		if len(agent.Delegates) > 0 {
			idx.Delegates = append([]string{}, agent.Delegates...)
		}
	}
	applyComponentIndexOverlay(idx, overlayFromAgent(agent))
	if idx.Title == "" {
		idx.Title = idx.Name
	}
	switch idx.Kind {
	case "mcp":
		if idx.MCP == nil {
			idx.MCP = &MCPIndex{Tools: []MCPTool{}}
		}
		if idx.MCP.Tools == nil {
			idx.MCP.Tools = []MCPTool{}
		}
		if len(idx.MCP.Tools) == 0 && agent != nil {
			for _, name := range agent.MCPService.Tools {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				idx.MCP.Tools = append(idx.MCP.Tools, MCPTool{
					Name:         name,
					InputSchema:  SchemaNotDeclared,
					OutputSchema: SchemaNotDeclared,
				})
			}
		}
		if idx.MCP.Transport == nil && agent != nil && strings.TrimSpace(agent.MCPService.Transport) != "" {
			idx.MCP.Transport = &MCPTransport{Type: normalizeMCPTransport(agent.MCPService.Transport)}
		}
		idx.Invoke.Contract = "mcp"
		idx.Agent = nil
		idx.HTTP = nil
		idx.Sandbox = nil
	case "tool":
		if idx.HTTP == nil {
			idx.HTTP = &HTTPIndex{Routes: []HTTPRoute{}}
		}
		if idx.HTTP.Routes == nil {
			idx.HTTP.Routes = []HTTPRoute{}
		}
		idx.Invoke.Contract = "http"
		idx.MCP = nil
		idx.Agent = nil
		idx.Sandbox = nil
	case "sandbox":
		if idx.Sandbox == nil {
			idx.Sandbox = &SandboxIndex{Capabilities: []string{}}
		}
		if idx.Sandbox.Capabilities == nil {
			idx.Sandbox.Capabilities = []string{}
		}
		idx.Invoke.Contract = "sandbox"
		idx.MCP = nil
		idx.Agent = nil
		idx.HTTP = nil
	default:
		if idx.Agent == nil {
			idx.Agent = &AgentIndex{
				LLM: []AgentLLM{},
				Invoke: AgentInvoke{
					Input:  "json:{message|query}",
					Output: "final_output + artifacts[]",
				},
			}
		}
		if len(idx.Agent.LLM) == 0 && agent != nil {
			if llm := agentLLMFromConfig(agent.LLM); llm != nil {
				idx.Agent.LLM = []AgentLLM{*llm}
			}
		}
		if idx.Agent.Invoke.Input == "" {
			idx.Agent.Invoke.Input = "json:{message|query}"
		}
		if idx.Agent.Invoke.Output == "" {
			idx.Agent.Invoke.Output = "final_output + artifacts[]"
		}
		idx.Invoke.Contract = "agent"
		idx.MCP = nil
		idx.HTTP = nil
		idx.Sandbox = nil
	}
	return idx
}

func mapComponentKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mcp", "mcp_service":
		return "mcp"
	case "tool", "http":
		return "tool"
	case "sandbox":
		return "sandbox"
	default:
		return "agent"
	}
}

func normalizeMCPTransport(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	if s == "" {
		return ""
	}
	return s
}

func overlayFromAgent(agent *AgentYAML) map[string]interface{} {
	if agent == nil || len(agent.ComponentIndex) == 0 {
		return nil
	}
	return cloneMap(agent.ComponentIndex)
}

func applyComponentIndexOverlay(idx *ComponentIndex, overlay map[string]interface{}) {
	if idx == nil || overlay == nil {
		return
	}
	if v := asTrimmedString(overlay["schema_version"]); v != "" {
		idx.SchemaVersion = v
	}
	if v := asTrimmedString(overlay["kind"]); v != "" {
		idx.Kind = mapComponentKind(v)
	}
	if v := asTrimmedString(overlay["title"]); v != "" {
		idx.Title = v
	}
	if v := asTrimmedString(overlay["name"]); v != "" {
		idx.Name = v
	}
	if v := asTrimmedString(overlay["version"]); v != "" {
		idx.Version = v
	}
	if v := asTrimmedString(overlay["description"]); v != "" {
		idx.Description = v
	}
	if hosts := stringList(overlay["egress"]); hosts != nil {
		idx.Egress = intersectEgressWithPolicy(hosts, idx.Egress)
	}
	if raw, ok := overlay["bindings"]; ok {
		idx.Bindings = parseBindings(raw)
	}
	if raw, ok := overlay["invoke"].(map[string]interface{}); ok {
		if v := asTrimmedString(raw["contract"]); v != "" {
			idx.Invoke.Contract = v
		}
		if v := asTrimmedString(raw["auth"]); v != "" {
			idx.Invoke.Auth = v
		}
	}
	if raw, ok := overlay["mcp"].(map[string]interface{}); ok {
		idx.MCP = mergeMCPIndex(idx.MCP, raw)
	}
	if raw, ok := overlay["agent"].(map[string]interface{}); ok {
		idx.Agent = mergeAgentIndex(idx.Agent, raw)
	}
	if raw, ok := overlay["http"].(map[string]interface{}); ok {
		idx.HTTP = mergeHTTPIndex(idx.HTTP, raw)
	}
	if raw, ok := overlay["sandbox"].(map[string]interface{}); ok {
		idx.Sandbox = mergeSandboxIndex(idx.Sandbox, raw)
	}
}

func mergeMCPIndex(base *MCPIndex, raw map[string]interface{}) *MCPIndex {
	out := &MCPIndex{Tools: []MCPTool{}}
	if base != nil {
		*out = *base
		if out.Tools == nil {
			out.Tools = []MCPTool{}
		}
	}
	if v := asTrimmedString(firstKey(raw, "protocolVersion", "protocol_version")); v != "" {
		out.ProtocolVersion = v
	}
	if caps, ok := raw["capabilities"].(map[string]interface{}); ok {
		out.Capabilities = cloneMap(caps)
	}
	if tr, ok := raw["transport"].(map[string]interface{}); ok {
		out.Transport = &MCPTransport{
			Type: normalizeMCPTransport(asTrimmedString(tr["type"])),
			URL:  asTrimmedString(tr["url"]),
		}
		if out.Transport.Type == "" && out.Transport.URL == "" {
			out.Transport = nil
		}
	}
	if tools := parseMCPTools(raw["tools"]); tools != nil {
		out.Tools = tools
	}
	if prompts := parseMCPPrompts(raw["prompts"]); prompts != nil {
		out.Prompts = prompts
	}
	if resources := parseMCPResources(raw["resources"]); resources != nil {
		out.Resources = resources
	}
	meta := map[string]interface{}{}
	if out.Meta != nil {
		for k, v := range out.Meta {
			meta[k] = v
		}
	}
	if declared, ok := raw["_meta"].(map[string]interface{}); ok {
		for k, v := range declared {
			meta[k] = v
		}
	}
	for k, v := range raw {
		if _, known := knownMCPKeys[k]; known {
			continue
		}
		meta[k] = v
	}
	if len(meta) > 0 {
		out.Meta = meta
	}
	return out
}

func mergeAgentIndex(base *AgentIndex, raw map[string]interface{}) *AgentIndex {
	out := &AgentIndex{
		LLM: []AgentLLM{},
		Invoke: AgentInvoke{
			Input:  "json:{message|query}",
			Output: "final_output + artifacts[]",
		},
	}
	if base != nil {
		*out = *base
	}
	if llms := parseAgentLLMs(raw["llm"]); llms != nil {
		out.LLM = llms
	}
	if inv, ok := raw["invoke"].(map[string]interface{}); ok {
		if v := asTrimmedString(inv["input"]); v != "" {
			out.Invoke.Input = v
		}
		if v := asTrimmedString(inv["output"]); v != "" {
			out.Invoke.Output = v
		}
	}
	return out
}

func mergeHTTPIndex(base *HTTPIndex, raw map[string]interface{}) *HTTPIndex {
	out := &HTTPIndex{Routes: []HTTPRoute{}}
	if base != nil {
		*out = *base
		if out.Routes == nil {
			out.Routes = []HTTPRoute{}
		}
	}
	if routes := parseHTTPRoutes(raw["routes"]); routes != nil {
		out.Routes = routes
	}
	return out
}

func mergeSandboxIndex(base *SandboxIndex, raw map[string]interface{}) *SandboxIndex {
	out := &SandboxIndex{Capabilities: []string{}}
	if base != nil {
		*out = *base
		if out.Capabilities == nil {
			out.Capabilities = []string{}
		}
	}
	if caps := stringList(raw["capabilities"]); caps != nil {
		out.Capabilities = caps
	}
	if tr, ok := raw["transport"].(map[string]interface{}); ok {
		out.Transport = &MCPTransport{
			Type: normalizeMCPTransport(asTrimmedString(tr["type"])),
			URL:  asTrimmedString(tr["url"]),
		}
	}
	return out
}

func parseMCPTools(raw interface{}) []MCPTool {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]MCPTool, 0, len(arr))
	for _, item := range arr {
		switch t := item.(type) {
		case string:
			name := strings.TrimSpace(t)
			if name == "" {
				continue
			}
			out = append(out, MCPTool{
				Name:         name,
				InputSchema:  SchemaNotDeclared,
				OutputSchema: SchemaNotDeclared,
			})
		case map[string]interface{}:
			name := asTrimmedString(t["name"])
			if name == "" {
				continue
			}
			out = append(out, MCPTool{
				Name:         name,
				Title:        asTrimmedString(t["title"]),
				Description:  asTrimmedString(t["description"]),
				InputSchema:  schemaOrNotDeclared(firstKey(t, "inputSchema", "input_schema")),
				OutputSchema: schemaOrNotDeclared(firstKey(t, "outputSchema", "output_schema")),
			})
		}
	}
	return out
}

func parseMCPPrompts(raw interface{}) []MCPPrompt {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]MCPPrompt, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := asTrimmedString(obj["name"])
		if name == "" {
			continue
		}
		p := MCPPrompt{
			Name:        name,
			Title:       asTrimmedString(obj["title"]),
			Description: asTrimmedString(obj["description"]),
		}
		if args, ok := obj["arguments"].([]interface{}); ok {
			for _, a := range args {
				am, ok := a.(map[string]interface{})
				if !ok {
					continue
				}
				an := asTrimmedString(am["name"])
				if an == "" {
					continue
				}
				req, _ := am["required"].(bool)
				p.Arguments = append(p.Arguments, MCPPromptArgument{Name: an, Required: req})
			}
		}
		out = append(out, p)
	}
	return out
}

func parseMCPResources(raw interface{}) []MCPResource {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]MCPResource, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		uri := asTrimmedString(obj["uri"])
		if uri == "" {
			continue
		}
		out = append(out, MCPResource{
			URI:         uri,
			Name:        asTrimmedString(obj["name"]),
			MimeType:    asTrimmedString(firstKey(obj, "mimeType", "mime_type")),
			Description: asTrimmedString(obj["description"]),
		})
	}
	return out
}

func parseHTTPRoutes(raw interface{}) []HTTPRoute {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]HTTPRoute, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		path := asTrimmedString(obj["path"])
		if path == "" {
			continue
		}
		method := asTrimmedString(obj["method"])
		if method == "" {
			method = "POST"
		}
		out = append(out, HTTPRoute{
			Method:       strings.ToUpper(method),
			Path:         path,
			InputSchema:  schemaOrNotDeclared(firstKey(obj, "inputSchema", "input_schema")),
			OutputSchema: schemaOrNotDeclared(firstKey(obj, "outputSchema", "output_schema")),
		})
	}
	return out
}

func parseAgentLLMs(raw interface{}) []AgentLLM {
	switch t := raw.(type) {
	case []interface{}:
		out := make([]AgentLLM, 0, len(t))
		for _, item := range t {
			obj, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if llm := agentLLMFromMap(obj); llm != nil {
				out = append(out, *llm)
			}
		}
		return out
	case map[string]interface{}:
		if llm := agentLLMFromMap(t); llm != nil {
			return []AgentLLM{*llm}
		}
	}
	return nil
}

func parseBindings(raw interface{}) []ComponentBinding {
	arr, ok := raw.([]interface{})
	if !ok {
		return []ComponentBinding{}
	}
	out := make([]ComponentBinding, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := asTrimmedString(firstKey(obj, "secret_name", "secretName"))
		if name == "" {
			continue
		}
		out = append(out, ComponentBinding{
			SecretName:  name,
			InjectAs:    asTrimmedString(firstKey(obj, "inject_as", "injectAs")),
			HostPattern: asTrimmedString(firstKey(obj, "host_pattern", "hostPattern")),
		})
	}
	return out
}

func agentLLMFromConfig(llm LLMConfig) *AgentLLM {
	if strings.TrimSpace(llm.Provider) == "" && strings.TrimSpace(llm.Model) == "" {
		return nil
	}
	return &AgentLLM{
		Provider:        strings.TrimSpace(llm.Provider),
		Model:           strings.TrimSpace(llm.Model),
		CredentialLabel: strings.TrimSpace(llm.Credential),
	}
}

func agentLLMFromMap(obj map[string]interface{}) *AgentLLM {
	provider := asTrimmedString(obj["provider"])
	model := asTrimmedString(obj["model"])
	if provider == "" && model == "" {
		return nil
	}
	return &AgentLLM{
		Provider:        provider,
		Model:           model,
		CredentialLabel: asTrimmedString(firstKey(obj, "credential_label", "credential")),
	}
}

func schemaOrNotDeclared(v interface{}) interface{} {
	if v == nil {
		return SchemaNotDeclared
	}
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return SchemaNotDeclared
		}
		return s
	}
	if m, ok := v.(map[string]interface{}); ok {
		return cloneMap(m)
	}
	return v
}

func asTrimmedString(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func firstKey(m map[string]interface{}, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func stringList(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s := asTrimmedString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// intersectEgressWithPolicy keeps overlay hosts that already exist on the
// signed agent policy. Overlay cannot grant a host the agent did not declare.
func intersectEgressWithPolicy(overlay, policy []string) []string {
	allowed := make(map[string]string, len(policy))
	for _, host := range policy {
		key := strings.ToLower(strings.TrimSpace(host))
		if key == "" {
			continue
		}
		if _, exists := allowed[key]; !exists {
			allowed[key] = host
		}
	}
	out := make([]string, 0, len(overlay))
	seen := make(map[string]struct{}, len(overlay))
	for _, host := range overlay {
		key := strings.ToLower(strings.TrimSpace(host))
		canon, ok := allowed[key]
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, canon)
	}
	return out
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]interface{}, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}
