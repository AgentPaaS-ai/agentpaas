// Package dashboard provides the embedded web dashboard for AgentPaaS.
// The dashboard is a Preact/TypeScript SPA compiled to static assets and
// embedded via go:embed. It is served by the daemon on a configurable port
// with strict Content-Security-Policy (no inline scripts, no CDN dependencies).
// API access requires a Bearer token; mutating routes require a CSRF token.
package dashboard
