# Model Allocation Template

Fill in the placeholders with your actual models, providers, and costs.
This is the single source of truth for role-to-model mapping in your OWA
pipeline.

## Role-to-Model Map

```yaml
# ORCHESTRATOR — judgment, dispatch, merge. Highest reasoning.
# Do NOT use a free/cheap model here unless trialed and proven.
orchestrator:
  model: <YOUR_HIGHEST_REASONING_MODEL>
  provider: <PROVIDER>
  cost_tier: flagship   # $0.50-3.00/call typical
  tools: [terminal, file, web, delegation, todo, memory]
  forbidden_tools: [patch, write_file]   # orchestrator does NOT edit code
  max_context: <YOUR_MODEL_CONTEXT_WINDOW>

# WORKER — code generation. Mid-tier, good at code.
worker:
  model: <YOUR_BEST_CODE_MODEL>
  provider: <PROVIDER>
  cost_tier: mid          # per-token generation
  tools: [terminal, file, code_execution]   # via CLI, not via orchestrator profile
  transport: cli          # e.g., codex exec, claude code, custom script
  sandbox: danger-full-access   # needs git, build, test, lint
  max_runtime: 2h

# ADVERSARY — destructive testing. Free or subscription model is fine.
# Trial before trusting: a free model that is "good on paper" may miss
# real breaks. Trial on one block; audit; keep only if it catches what
# the Flagship model catches.
adversary:
  model: <YOUR_FREE_OR_SUBSCRIPTION_MODEL>
  provider: <PROVIDER>
  cost_tier: subscription   # $0/call
  tools: [terminal, file]   # ONLY these two. Nothing else.
  forbidden_tools: [web, browser, vision, delegation, code_execution, patch]

# VERIFIER — integration assurance. Same tier as orchestrator usually.
# Runs ONCE per block, NOT per subtask. Fresh context.
verifier:
  model: <YOUR_HIGHEST_REASONING_MODEL>
  provider: <PROVIDER>
  cost_tier: flagship
  tools: [terminal, file]   # ONLY these two.
  forbidden_tools: [web, browser, vision, delegation, code_execution, patch]
  frequency: once-per-block

# DOC_SUBAGENT — mechanical summarization. Cheapest fast model.
# Reads diffs, returns 5-line summaries. Never used for judgment.
doc_subagent:
  model: <YOUR_CHEAPEST_FAST_MODEL>
  provider: <PROVIDER>
  cost_tier: cheap
  tools: [terminal, file, read_only]
```

## Hard Rules

1. **No silent fallback.** If `orchestrator.model` is unavailable, halt.
   Do not substitute `worker.model` or `adversary.model` for it. A
   silent Orchestrator fallback reintroduces judgment failures (missed
   breaks, wrong merges) that motivated the role split.

2. **Orchestrator has no file-patch tool.** `forbidden_tools: [patch,
   write_file]` is enforced at the tool-schema level, not just the prompt
   level. If the orchestrator has the tool, it will use it under pressure.

3. **Adversary and Verifier have ONLY terminal + file.** No browser, no
   web, no vision, no delegation. They read code and run tests. Nothing
   else.

4. **Verifier runs once per block.** Not per subtask. A per-subtask
   Verifier catches what the Adversary already caught, or noise. It
   costs ~89% more API calls for zero novel value.

5. **Trial before trusting.** Before assigning a model to a role
   permanently, run it on one block and audit:
   - Orchestrator: did it catch every Adversary break? Did it make any
     wrong merge calls? Did it scope tasks correctly?
   - Adversary: did it find real breaks (not just noise)? How many
     confirmed-safe tests did it write?
   - Worker: did it pass the gate on first attempt >50% of the time?

## Cost Estimation Template

Per block (5 subtasks):

```
orchestrator:  50 calls × $<TIER_HIGH>/call   = $<calculated>
worker:        5 dispatches × $<TIER_MID>/gen  = $<calculated>
adversary:     5 dispatches × $0                = $0
verifier:      1 call × $<TIER_HIGH>/call       = $<calculated>
doc_subagent:  10 calls × $<TIER_LOW>/call      = $<calculated>
                                  TOTAL PER BLOCK = $<sum>
```

Target: <$15/block for a 5-subtask block with pruning + local-first.
Without pruning or with per-subtask CI: $40+/block.
