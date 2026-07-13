# AgentPaaS Golden Loop Test

**Pinned to:** v0.2.3
**Last updated:** 2026-07-12
**Purpose:** End-to-end manual test that exercises the full AgentPaaS lifecycle —
clean install, agent build, modify, provenance, export, teardown, receive-as-friend,
inspect, install, and run. This is the release gate test. Every public release
must pass the golden loop before shipping.

## How to Use

1. Run nuclear teardown to start from zero.
2. Execute each phase in order. Do not skip.
3. Verify each phase on disk (invoke-response.json, harness-audit.jsonl,
   policy.yaml, bundle inspect) before proceeding to the next.
4. On failure: STOP, report, do not attempt local fixes. Log the bug.
5. Mark PASS in `b24.5-manual-testing-plan.md` with build provenance.
6. Update this file for each new release — add new phases or adjust existing
   ones as features are added. Pin to the release version being tested.

## Prerequisites

- OpenRouter API key (stored at `/tmp/openrouter-key.txt` or entered by user)
- Hermes Agent installed
- The `agentpaas-manual-testing-setup` skill loaded by the orchestrator

## Test Session vs Orchestrator

- **Test session** (`hermes -p agentpaas-testing`): the Hermes session that
  simulates a real user. All agent building, packing, and running happens here.
- **Orchestrator** (this agent): verifies on disk after each phase. Reads
  SQLite session DB to review what the test session did. Never fabricates
  results — always reads invoke-response.json and harness-audit.jsonl.

---

## Phase 1: Clean Slate

**Action:** Run nuclear teardown.

```bash
bash ~/.hermes/profiles/agentpaas/skills/devops/agentpaas-manual-testing-setup/scripts/nuclear-teardown.sh
```

**Verify:**
- `which agentpaas` → NOT FOUND
- `which colima` → NOT FOUND
- `which docker` → NOT FOUND
- No plugin at `~/.hermes/profiles/agentpaas-testing/plugins/agentpaas/`
- No skill at `~/.hermes/profiles/agentpaas-testing/skills/agentpaas/`
- No `agentpaas` in `config.yaml` platform_toolsets
- No leftover skills (agentpaas, agentpaas-lifecycle, devops/agentpaas-setup)

---

## Phase 2: First-Time Install (Publisher)

**Action:** Start test session and paste the install prompt.

```bash
hermes -p agentpaas-testing
```

```
Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas
```

**Expected agent behavior:**
1. Load hermes-plugin-install skill (will clone repo — this is expected)
2. Read after-install.md — follow Step 0 prerequisite check
3. Detect all 3 binaries missing (agentpaas, colima, docker)
4. Install: `brew install colima docker`, `colima start`,
   `brew install agentpaas-ai/tap/agentpaas` (v0.2.3)
5. `xattr -cr` all 3 binaries (MANDATORY before any agentpaas command)
6. `agentpaas daemon start`
7. `agentpaas doctor` — 7/7 checks pass
8. `hermes plugins install --enable https://github.com/AgentPaaS-ai/agentpaas`
9. Run `ensure-toolset.py` to register toolset
10. Create skill pointer at `skills/agentpaas/SKILL.md`
11. Tell user to restart Hermes. STOP. No build offers.

**Verify on disk:**
- `which agentpaas` → `/opt/homebrew/bin/agentpaas`
- `agentpaas version` → CLI 0.2.3 (commit SHA)
- `agentpaas doctor` → 7/7
- `plugins/agentpaas/plugin.yaml` exists
- `agentpaas` in `config.yaml` platform_toolsets.cli
- `skills/agentpaas/SKILL.md` exists

---

## Phase 3: Doctor Health Check

**Action:** Restart test session, paste doctor prompt.

```
Run agentpaas doctor
```

**Verify:** All checks pass (version, Docker CLI, Docker daemon, Keychain,
harness, home directory, skopeo optional).

---

## Phase 4: Build Weather Agent

**Action:** Paste the build prompt.

```
Build a weather agent that takes a city name as input, uses an LLM to look up the weather, and returns the current conditions.
```

**Expected agent behavior:**
1. Load `agentpaas-build` skill (via skill pointer)
2. Ask: "Which LLM provider?" → user: openrouter
3. Ask: "Which model?" → user: deepseek/deepseek-v4-flash
4. Tell user to run `agentpaas secret add openrouter-key` in separate terminal
5. User pipes key: `cat /tmp/openrouter-key.txt | agentpaas secret add openrouter-key`
6. Verify secret via `agentpaas_secret_list`
7. Check publisher identity — tell user to `agentpaas identity init` if missing
   (name: parvezsyed)
8. Confirm egress hostnames only (no ports): "wttr.in, openrouter.ai. Allow?"
9. Scaffold project at `~/weather-agent`
10. Write `main.py` with:
    - `from agentpaas_sdk import agent` (module-level singleton)
    - `@agent.on_invoke` decorator
    - `agent.http("GET", ...)` to fetch wttr.in data
    - `agent.llm(prompt=...)` to summarize (NOT `http_with_credential`)
    - Return `{"status": "OK", "answer": ...}`
11. Write `policy.yaml` with `domain:` / `ports:` schema (NOT `host:` or `hostname:`)
12. Configure LLM via `agentpaas_llm_configure`
13. Pack, run, invoke with a city
14. Verify real data via `agentpaas_status`

**Verify on disk:**
- `invoke-response.json`: status=OK, answer has LLM summary with real weather
- `harness-audit.jsonl`: egress_allowed for wttr.in (200) + openrouter.ai (200), 0 denials
- `main.py`: uses `agent.llm()`, `@agent.on_invoke`, http fetch then LLM summarize
- `policy.yaml`: `domain: wttr.in, ports: [443]` + `domain: openrouter.ai, ports: [443]`

**Known issues to watch for:**
- Agent may write `host:` instead of `domain:` in policy.yaml (Bug 018 variant)
- Agent may use `http_with_credential` instead of `agent.llm()` (should self-correct)
- Wall clock budget is 120s (was 30s, caused LLM timeouts)

---

## Phase 5: Modify Agent (Add News)

**Action:** Paste the modify prompt.

```
Add a Google News lookup to the weather agent — fetch news headlines for the same city and include them in the response. Repack and retest.
```

**Expected agent behavior:**
1. Read existing main.py
2. **Confirm new hostname** before adding to policy: "This agent will now also
   access: news.google.com. Allow?" (Bug 030 — agent may skip this)
3. Modify main.py to fetch Google News RSS
4. Update policy.yaml with `domain: news.google.com, ports: [443]`
5. Repack (new digest)
6. Run, invoke, verify

**Verify on disk:**
- New image digest differs from Phase 4
- `policy.yaml`: has `news.google.com` with `domain:` + `ports: [443]`
- `harness-audit.jsonl`: 3 egress_allowed (wttr.in, news.google.com, openrouter.ai), 0 denials
- `invoke-response.json`: real news headlines in response
- wttr.in cross-check: weather data matches

**Bug 030:** If agent does NOT confirm hostname before adding to policy, log it.

---

## Phase 6: Switch Weather API

**Action:** Paste the switch prompt.

```
Switch the weather agent to use Open-Meteo instead of wttr.in for weather data. Remove wttr.in from the egress policy and add the Open-Meteo API hostname. Repack and retest with Folsom.
```

**Expected agent behavior:**
1. Confirm new hostnames (geocoding-api.open-meteo.com, api.open-meteo.com)
2. Modify main.py to use Open-Meteo API
3. Update policy.yaml: remove wttr.in, add open-meteo domains
4. Repack, run, invoke with Folsom

**Verify on disk:**
- `policy.yaml`: wttr.in removed, open-meteo domains added
- `harness-audit.jsonl`: egress_allowed for open-meteo domains + openrouter.ai
- `invoke-response.json`: real weather data from Open-Meteo

---

## Phase 7: Provenance and Audit

**Action:** Paste the provenance prompt.

```
Show the provenance and lineage audit for the weather agent.
```

**Expected agent behavior:**
1. Call `agentpaas_bundle_inspect` or provenance commands
2. Show publisher name (parvezsyed), fingerprint
3. Show creation history, chain entries
4. Show policy summary

**Verify:**
- Provenance shows created by parvezsyed
- Fingerprint matches `agentpaas identity show`
- Chain has at least 1 entry (created)

---

## Phase 8: Export Bundle

**Action:** Paste the export prompt.

```
Export the weather agent so I can share it with a friend.
```

**Expected agent behavior:**
1. Verify publisher identity exists
2. Call `agentpaas_export`
3. Return bundle path, digest, fingerprint
4. Tell user to relay fingerprint out-of-band

**Verify on disk:**
- Bundle file exists (`.agentpaas`)
- `agentpaas bundle inspect <bundle>` → ALL 9 checks PASS
  (manifest_parse, manifest_signature, publisher_match, lock_provenance,
   content_sha256, policy_digest, sbom_digest, **source_digest**, image_digest)
- Publisher name and fingerprint shown
- Copy bundle to `/Users/pms88/received-weather-agent.agentpaas`

**Critical:** source_digest MUST pass. If it fails, the export is broken.

---

## Phase 9: Nuclear Teardown (Receiver)

**Action:** Wipe everything.

```bash
bash ~/.hermes/profiles/agentpaas/skills/devops/agentpaas-manual-testing-setup/scripts/nuclear-teardown.sh
```

**Verify:** Same as Phase 1 — all binaries, state, Keychain, profile stripped.

---

## Phase 10: First-Time Install (Receiver)

**Action:** Same as Phase 2 — fresh install from scratch.

```bash
hermes -p agentpaas-testing
```

```
Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas
```

**Verify:** Same as Phase 2.

---

## Phase 11: Receive and Install Bundle

**Action:** Restart test session, paste the install prompt.

```
I received an agent bundle from a friend at /Users/pms88/received-weather-agent.agentpaas. Can you install it?
```

**Expected agent behavior:**
1. **Inspect bundle FIRST** — even though user didn't ask (D3 language rules)
2. Show what the agent CAN ACCESS (list every egress domain)
3. Show what credentials it needs (openrouter-key)
4. Show publisher name and fingerprint
5. Tell user to verify fingerprint out-of-band
6. NOT auto-install or auto-trust
7. Check which credentials user has — guide `agentpaas secret add openrouter-key`
8. Tell user to run `agentpaas install` in terminal
9. Verify install succeeded

**Verify on disk:**
- `agentpaas bundle inspect` → ALL 9 checks PASS
- Agent appears in installed list
- Credential mapped (openrouter-key)

**Note:** Publisher identity will differ between publisher (Phase 4) and receiver
because nuclear teardown wipes Keychain. The receiver creates their own identity.
The bundle's signature still verifies against the publisher's public key embedded
in the bundle.

---

## Phase 12: Run Installed Agent

**Action:** Agent should offer to test run. If not, paste:

```
Run the installed weather agent with city=Folsom.
```

**Verify on disk:**
- `invoke-response.json`: status=OK, real weather + news data
- `harness-audit.jsonl`: egress_allowed for all expected domains, 0 denials

---

## Bug Log

Bugs found during golden loop testing. Update for each release.

| Bug ID | Phase | Priority | Description | Status |
|--------|-------|----------|-------------|--------|
| 018 | 4 | P2 | Agent writes host/hostname instead of domain in policy.yaml, self-corrects after pack failure | open |
| 024 | 4 | minor | SDK agent.llm() returns dict with key "text" but no docs guide agent authors | open |
| 030 | 5 | security/UX | Agent doesn't confirm new egress hostname when modifying existing agent | open |

---

## Release Gate

**GO** — all 12 phases pass, no blocking bugs. Minor bugs logged for future fixes.

**NO-GO** — any phase fails. Fix, rebuild, re-release, re-test from Phase 1.

---

## Update Protocol

When a new release is cut:
1. Update the "Pinned to" version at the top
2. Add any new phases for new features
3. Adjust existing phases if behavior changed
4. Run the full golden loop from Phase 1
5. Update bug log with any new bugs found
6. Mark release gate GO/NO-GO
