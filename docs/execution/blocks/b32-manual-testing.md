# B32 / pre-v0.3.0 Manual Testing Checklist

Use a **clean PATH that prefers repo bins**, not Homebrew 0.2.3:

```bash
export PATH="$HOME/projects/agentpaas/bin:$PATH"
hash -r
which agentpaas agentpaasd   # must be .../projects/agentpaas/bin/...
agentpaas version            # must show CLI: 0.3.0-dev (NOT 0.2.3 or 0.1.0-dev)
```

Rebuild + daemon:

```bash
cd ~/projects/agentpaas
make build-all
bash scripts/check-release-versions.sh   # must PASS
rm -f ~/.agentpaas/daemon.sock ~/.agentpaas/agentpaasd.pid ~/.agentpaas/agentpaasd.lock
./bin/agentpaasd &
sleep 1
agentpaas doctor
```

## M1 — Fresh version hygiene
- [ ] `agentpaas version` → 0.3.0-dev
- [ ] `strings bin/agentpaas | grep -E '0\.1\.0-dev|0\.2\.[0-9]'` shows no stale **dev** stamps (ignore historical changelog text in other files)
- [ ] Homebrew ` /opt/homebrew/bin/agentpaas version` may still say 0.2.3 — confirm you are **not** using it for this test

## M2 — Registry + promote (B31)
- [ ] Pack/install a small agent (demo/weather-agent or fresh init)
- [ ] `agentpaas registry list` / `registry show`
- [ ] `agentpaas registry promote <name>@...`
- [ ] Un-promoted package cannot be named in a workflow pack that requires promotion

## M3 — Delegation surface (B32 code path)
Where durable invoke/harness is available:
- [ ] Agent code can call logical `delegate(capability=...)` (SDK) without host/port params
- [ ] Response/task JSON has no IP, localhost, network_alias, capability_token
- [ ] Unknown binding denied with stable code
- [ ] Idempotent retry with same key returns same task id (or documented behavior)

## M4 — Artifact transfer
- [ ] Commit artifact → project to authorized audience succeeds
- [ ] Wrong audience denied
- [ ] Tamper blob on disk → verify/project fails

## M5 — Wait/wake
- [ ] Parent can disconnect and resume from event cursor without polling loops
- [ ] Terminal event delivered once under duplicate delivery

## M6 — Negative paths
- [ ] Undeclared delegation denied and audited
- [ ] Audience-mismatched artifact read denied

## M7 — Golden / gate
```bash
export PATH="$HOME/projects/agentpaas/bin:$PATH"
make golden-fast
make block32-gate   # long; optional if already green this session
```

## Release (DO NOT run until founder says go)
```bash
# only after this checklist is green and founder approves:
# git tag v0.3.0 && git push origin v0.3.0
# then publish draft GH release + brew cask
```

Never retag a failed v0.3.0 — use v0.3.1.
