# Privacy & Telemetry

AgentPaaS is designed with privacy as a first principle. **Zero telemetry, zero
phone-home** is the default and only behavior in the current release.

## What We Don't Do

- **No analytics.** No usage data, session metrics, or feature telemetry is
  collected or transmitted.
- **No crash reports.** Crashes stay on your machine. No stack traces, core
  dumps, or error logs are sent anywhere.
- **No update checks.** AgentPaaS does not ping any remote server to check for
  new versions.
- **No telemetry SDKs.** The agent runtime, gateway, and CLI contain no
  third-party analytics, crash-reporting, or telemetry libraries.

## Local-First by Design

All processing is local. The agent runs in a Docker container on your machine.
The gateway sidecar, policy engine, and audit system operate entirely on-host.
No data leaves your machine unless the agent explicitly makes an outbound call
subject to your policy rules.

## Audit Data

The signed audit chain is stored at `~/.agentpaas/state/` on your machine.
Audit exports (when you explicitly request them) produce verifiable,
cosign-signed bundles — but the export action is always initiated by you.

## Future Telemetry

If any telemetry is introduced in a future release, it will be:
- **Explicit opt-in** — off by default, never enabled without your consent
- **Separately packaged** — not bundled into the core runtime
- **Clearly documented** — what is collected, why, and where it goes