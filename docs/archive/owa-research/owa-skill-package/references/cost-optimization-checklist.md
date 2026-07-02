# Cost Optimization Checklist

All cost-optimization steps in one place. Run through this before each
block. The goal is to minimize the two largest controllable line items:
agent attention (context tokens) and cloud round-trips (CI/API latency).

## 1. Local-First Execution

- [ ] Worker commits to a local branch (no PR, no push per subtask)
- [ ] Gate runs locally (no CI dispatch per subtask)
- [ ] Adversary runs locally
- [ ] Merge is local (no merge API per subtask)
- [ ] Push happens ONCE at block-end (checkpoint), not per subtask

Expected savings: ~50% wall clock, ~65% token cost, 100% GitHub API calls

## 2. Context Pruning (Before Block Start)

- [ ] Disable toolsets the build does not use:
      vision, video, image_gen, video_gen, tts, browser, computer_use,
      social media, music, home automation, clarify, cronjob
- [ ] Keep only: terminal, file, code_execution, delegation, todo, memory
- [ ] Remove MCP servers the orchestrator does not call
- [ ] Move irrelevant skills to a holding directory (reversible)
- [ ] Adversary/Verifier profiles: ONLY terminal + file. Nothing else.

Expected savings: ~20KB/call, ~250K tokens/block, ~99% per-subtask context

## 3. Parallel Gate Execution

- [ ] Gate components run concurrently (build, test, race, lint, secscan)
- [ ] Each component logs to a separate file
- [ ] Poll for completion; check each log
- [ ] For Docker e2e: split into 4-5 batches with unique network IDs

Expected savings: 3-5 min sequential → ~1 min parallel (slowest component)

## 4. Path-Filtered CI (If Using CI)

- [ ] Each block gate has a path filter (only runs on its own code change)
- [ ] Shared files (build config, dependency manifests, proto/API defs)
      trigger all gates
- [ ] Single-package change runs only that package's gate

Expected savings: skip slow Docker gate (~10 min) for localized changes

## 5. Self-Hosted CI Runner (If Using CI)

- [ ] Runner is on the dev machine (or same hardware class)
- [ ] All tools pre-installed (no per-job install step)
- [ ] runs-on: self-hosted (not ubuntu-latest)
- [ ] macOS caution: GitHub Actions macOS runners bill at 10x. Self-hosting
      eliminates this.

Expected savings: 0 billed minutes, 3-29x faster per job

## 6. Verifier Right-Sizing

- [ ] Verifier runs ONCE per block (block-end, fresh context)
- [ ] NOT per subtask (per-subtask Verifier caught 0 novel issues in our audit)
- [ ] Focused re-verification on fix cycles (only affected criteria, not
      full re-verify)

Expected savings: ~89% Verifier API calls, ~65% fix-cycle time

## 7. Adversary on Free/Subscription Model

- [ ] Adversary uses a $0/call model (subscription or free tier)
- [ ] Trialed on one block before permanent assignment
- [ ] Catches real breaks (not just noise)

Expected savings: 100% of adversary API budget

## 8. No Per-Subtask PRs

- [ ] No PR creation per subtask (local-first mode)
- [ ] No CI dispatch per subtask
- [ ] No status reporting per subtask
- [ ] No merge API per subtask
- [ ] PRs (if needed) created as a batch at block-end

Expected savings: 15-30 min overhead per subtask eliminated

## 9. Doc/Summary Subagent Delegation

- [ ] PR diff reading delegated to a cheap subagent (returns 5-line summary)
- [ ] Orchestrator never reads full PR diffs in its session
- [ ] Doc writing delegated to a cheap subagent
- [ ] Long strings written to /tmp files, not inline in orchestrator output

Expected savings: orchestrator context stays small; no inline bloat

## 10. One Session Per Block (Not Per Subtask)

- [ ] Per-subtask context is ~5KB (Codex JSON + gate result + adversary report)
- [ ] Entire block runs in ONE orchestrator session
- [ ] If context grows above ~30K tokens, prune harder or split at a natural
      breakpoint

Expected savings: 5 sessions → 1 session per block; no repeated system prompt
re-shipping
