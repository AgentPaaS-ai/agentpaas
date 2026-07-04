# B16 Fix Round 3 — Resume Prompt

**Date:** 2026-07-03
**Session:** Continuing from B16 manual test round 3
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 4c85edb (all T4-01..T4-13 fixes pushed)

## START HERE

Fix the bugs below in priority order. Use OWA (Grok workers via tmux
for implementation, orchestrator reviews + merges). All work goes
through workers — orchestrator only plans, dispatches, reviews, merges.

Load skills before starting: `agentpaas-build-rhythm`,
`agentpaas-owa-build-orchestration`, `agentpaas-acceptance-testing`,
`owa-multi-agent-coding`, `grok`.

## BINARIES

Current binaries at /usr/local/bin/agentpaas{,d} are from 4c85edb.
After each fix: rebuild with `go build -a`, install, restart daemon.
**IMPORTANT:** Always use `go build -a` (full rebuild). `make build` can
produce stale cached binaries. Verify with `md5` comparison.

```bash
cd ~/projects/agentpaas
go build -a -o bin/agentpaas ./cmd/agentpaas
go build -a -o bin/agentpaasd ./cmd/agentpaasd
sudo cp -f bin/agentpaas bin/agentpaasd /usr/local/bin/
agentpaas daemon stop; sleep 2; rm -f ~/.agentpaas/daemon.sock
agentpaas daemon start
```

## WHAT TO FIX (in priority order)

### T4-16 (CRITICAL): Gateway allowlist must be synced from agent policy.yaml

**Problem:** The agentgateway (egress HTTP proxy on port 7799) rejects
ALL outbound traffic with "network authorization denied". The gateway's
config.yaml has a hardcoded allowlist (e.g. `api.weather.gov`) that
doesn't match the agent's policy.yaml egress rules.

**Root cause:** When `agentpaas run` starts a run, it creates the gateway
container with a default/fixed config. It does NOT generate the gateway
config FROM the agent's policy.yaml.

**Files to investigate:**
- `internal/daemon/control_handlers.go` — the Run handler (creates
  containers, networks, gateway)
- `internal/runtime/docker.go` — container/network creation
- Search for where the gateway config.yaml is generated/written
- Search for `agentgateway` or `gateway` config generation

**Fix approach:**
1. Find where the gateway container is created during `Run`
2. Before starting the gateway, read the agent's policy.yaml
3. Generate a gateway config.yaml with `networkAuthorization` rules
   matching the policy's egress allowlist
4. Write/mount this config into the gateway container

**Gateway config format** (from inspection):
```yaml
binds:
  - port: 7799
    listeners:
      - protocol: HTTP
        routes:
          - name: egress
            backends:
              - host: <domain>:443   # for each allowed domain
frontendPolicies:
    networkAuthorization:
      - allow: dns.domain == "<domain>"  # for each allowed domain
```

**Verification:**
1. Create agent with `allow-http` policy (allows *:443)
2. Pack + run
3. From inside the agent container, test HTTPS to an allowed domain
4. Should succeed (not "connection reset by peer")

### T4-17 (HIGH): Agent must confirm egress destinations with user

**Problem:** When building an agent, Hermes doesn't ask the user which
external domains the agent should be allowed to access.

**Fix:** Update `integrations/hermes-plugin/SKILL.md` with explicit
build-time instructions:
1. Analyze agent code for external domains (URLs, API endpoints)
2. Present domains to user: "This agent will access X, Y, Z. Allow?"
3. Generate policy.yaml with ONLY confirmed domains (not wildcard)
4. Never use `allow-http` (wildcard *:443) unless user explicitly
   requests it

### T4-18 (HIGH): Agent must procure API keys/OAuth from user

**Problem:** When an agent needs external API keys, Hermes doesn't ask
the user for them.

**Fix:** Update SKILL.md with credential procurement flow:
1. Detect credential needs in agent code (env vars, auth headers)
2. Ask user: "This agent needs an API key for X. Do you have one?"
3. Store via `agentpaas_secret_add`
4. Reference in agent.yaml

### T4-19 (HIGH): Agent must ask which LLM to use

**Problem:** When deploying an agent that needs an LLM, Hermes doesn't
ask the user which provider/model/key to use.

**Fix:** Update SKILL.md with LLM configuration flow:
1. Recognize agent needs LLM (imports openai/anthropic, has chat logic)
2. Ask: "Which LLM provider? (OpenAI/Anthropic/xAI)"
3. Ask: "Which model?"
4. Ask for API key if not stored
5. Configure agent.yaml llm: section
6. Add egress for chosen provider domain to policy

### T4-14 (MED): reconcile_project fails with absolute paths

**Problem:** `agentpaas init /tmp/project --from-code --noninteractive`
fails with "project path must be under current directory".

**Fix:** Allow absolute paths in init --from-code. File: probably
`internal/cli/init.go` or `internal/pack/init.go`.

### T4-15 (LOW): recommend-patch parser improvements

**Problem:** Only recognizes "allow egress to <domain>". Other phrasings
return empty patches.

**Fix:** Improve parser in the recommend-patch handler. Accept domain +
port as structured input if NL parsing fails.

## BUILD DISCIPLINE

- Each fix is a separate micro-chunk commit
- After each fix: `go build -a && go test ./internal/... -short`
- After all fixes: full E2E test with weather agent (must reach the API)
- Push each commit to main
- Use Grok workers (grok-composer-2.5-fast, $0) via tmux

## OWA MODEL ALLOCATION

- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Workers: grok-composer-2.5-fast via Grok CLI ($0, subscription)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile (block-end)

## VERIFICATION (after ALL fixes)

The critical test: build a weather agent from scratch via the
agentpaas-test profile. The agent must:
1. Ask the user which weather API to use
2. Ask the user to confirm egress to that API
3. Ask the user if they have an API key (if needed)
4. Ask the user which LLM to use (if the agent needs one)
5. Deploy the agent
6. Invoke it — the agent must SUCCESSFULLY reach the weather API
7. Return actual weather data

If the weather agent returns real data, ALL bugs are fixed.

## CONTEXT FROM PRIOR SESSIONS

- See `docs/b16-manual-test-round2-findings.md` for T4-01..T4-13 bugs (ALL FIXED)
- See `docs/b16-manual-test-round3-findings.md` for T4-14..T4-19 bugs (TO FIX)
- See `docs/b16-fix-round1-complete.md` for RC1-RC6 fixes (ALL MERGED)
- agentpaas-test profile is currently set up with plugin + toolset
- Daemon is running, binaries at 4c85edb

## KEY PITFALLS

- `make build` can produce stale binaries. Always `go build -a`.
- Verify binary installation with `md5 bin/agentpaasd /usr/local/bin/agentpaasd`
- After daemon restart, check `agentpaas status` shows "ready"
- Concurrent run limit is 3. Stop old runs before starting new ones.
- The gateway config format uses agentgateway v1.3.0 syntax.
  See: https://github.com/agentgateway/agentgateway
