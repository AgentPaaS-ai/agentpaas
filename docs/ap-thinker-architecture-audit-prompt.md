# Architecture Audit Prompt for ap-thinker (Kimi-K3)

## Context

You are running as the ap-thinker profile (Kimi-K3, 1M context window). Your role is read-only architectural analysis. You do not write code. You do not fix things. You do not modify files. You do not run builds. You audit, you critique, you tell the founder what is over-engineered, what is missing, and what is just right. Your sole output is the audit report as your response text.

## The situation

AgentPaaS is a platform for running AI agents securely inside isolated containers with default-deny egress, credential brokering, and tamper-evident audit. The product pitch, 66 approved decisions (D1-D66), and a 16-block execution plan (B26-B41) were built with help from GPT-5.6. The founder suspects the plan may be over-engineered. B1-B25 shipped as v0.2.3. B26 and B27 are implemented. B28-B41 are execution-ready specs but not yet built.

The goal is to ship v0.3, v0.4, and v0.5 as a secure, working product that wows the LinkedIn and developer audience and motivates them to try AgentPaaS.

## Your task

Read every file listed below in full. Do not skim. Do not skip sections. These are full execution-ready specifications, not summaries. Some are 600-1700 lines. Read every line.

### Files to read (in order)

1. Product pitch and all 66 decisions:
   /Users/pms88/projects/agentpaas/Agentpaas-pitch.md

2. Roadmap:
   /Users/pms88/projects/agentpaas/docs/roadmap.md

3. Execution plan, block status table, and release sequence:
   /Users/pms88/projects/agentpaas/docs/execution/README.md

4. Every block spec (read each one in full, line by line):
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b16-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b17-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b18-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b19-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b20-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b21-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b22-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b23-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b24-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b25-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b26-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b27-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b28-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b29-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b30-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b31-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b32-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b33-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b34-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b35-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b36-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b37-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b38-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b39-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b40-summary.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b41-summary.md

5. Supplementary block docs (review notes, coupling inventories, parked notes, substrate decisions):
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b28-review-notes.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b28-t01-coupling-inventory.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b28-t07-substrate-decision.md
   /Users/pms88/projects/agentpaas/docs/execution/blocks/b30-parked-notes.md

6. OWA records (completed block evidence):
   /Users/pms88/projects/agentpaas/docs/owa-records/b22-t02.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b22-t03.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b22-t04.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-t01.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-t02.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-t03.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-t04.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-t05.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-t06.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b23-block-end.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b24-t01.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b24-t02.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b24-t03.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b24-t04.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b24-t05.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b26-t02.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b26-t04.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t01.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t02.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t03.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t04.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t05.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t06.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-t07.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b27-block-end.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b29-t01.md
   /Users/pms88/projects/agentpaas/docs/owa-records/b29-block-end.md

7. Risk analyses for completed blocks:
   /Users/pms88/projects/agentpaas/docs/b22-risk-analysis.md
   /Users/pms88/projects/agentpaas/docs/b23-risk-analysis.md
   /Users/pms88/projects/agentpaas/docs/b24-risk-analysis.md
   /Users/pms88/projects/agentpaas/docs/b26-risk-analysis.md
   /Users/pms88/projects/agentpaas/docs/b27-risk-analysis.md

8. Security and architecture docs:
   /Users/pms88/projects/agentpaas/docs/threat-model.md
   /Users/pms88/projects/agentpaas/docs/trust-model.md
   /Users/pms88/projects/agentpaas/docs/how-enforcement-works.md
   /Users/pms88/projects/agentpaas/docs/known-limitations.md
   /Users/pms88/projects/agentpaas/docs/secrets.md
   /Users/pms88/projects/agentpaas/docs/policy-reference.md
   /Users/pms88/projects/agentpaas/docs/bundle-format.md

9. Golden Loop test (the release gate):
   /Users/pms88/projects/agentpaas/docs/execution/golden-loop-test.md

10. Repo README (what the public sees):
    /Users/pms88/projects/agentpaas/README.md

That is approximately 40+ files totaling 15,000+ lines. Read them all before forming a single opinion.

## What to produce

A single architecture audit report as your response text. Do not write any files. Do not modify any files. Do not run any commands. Structure the report as follows:

### Part 1: Per-Block Deep Audit (B16 through B41)

For each block, read the full spec and then provide:

- Block number and title
- Classification: CRITICAL / VALUABLE / COSMETIC / USELESS
- Detailed reasoning (5-10 sentences): What does this block actually do? What specific decisions (cite D-numbers) does it implement? Is the scope proportionate to the value? What happens if it is cut, simplified, or deferred?
- Specific over-engineering observations: Quote or paraphrase specific spec language that is more complex than needed. Do not be vague. If a spec says "Cloudflare proof environment," say why that is unnecessary for v0.3. If a spec says "three activation classes," say which ones matter and which do not.
- Dependencies: Does this block depend on others that are also questionable? Would cutting it simplify downstream blocks?
- Verdict: KEEP AS-IS / KEEP BUT SIMPLIFY / MERGE WITH (block X) / DEFER TO POST-v0.5 / DROP

Definitions:
- CRITICAL: Essential for security, core product function, or the founder's stated goals. Cannot be cut without breaking the product promise.
- VALUABLE: Adds real capability that the target audience (LinkedIn devs, early adopters) will notice and appreciate. Keep, but may be simplified.
- COSMETIC: Nice to have, polish, or speculative architecture that does not materially affect the v0.3-v0.5 user experience. Flag for removal or deferral.
- USELESS: Over-engineered, unnecessary, or building infrastructure for a scale or scenario that is not the current product. Flag for removal with reasoning.

### Part 2: Cross-Block Over-Engineering Patterns

List every recurring pattern across multiple blocks that adds complexity a v0.3-v0.5 product targeting early adopters does not need. For each pattern:

- Name the pattern
- List which blocks exhibit it (cite specific spec language from each)
- Explain why it is over-engineered for this stage
- Say what to do instead

Examples to look for:
- Substrate proofs that exist for their own sake (Cloudflare feasibility, Kubernetes conformance when Docker is the only shipped runtime)
- Multi-tenant abstractions before there is a second tenant
- Elaborate catalog resolution when a flat list would work for the demo path
- Bounded parent/child orchestration when a single-agent invocation is the demo path
- Deterministic model routing when the user just needs "it works with the model I configured"
- Spend ledgers before there is meaningful spend
- Activation profiles (on_demand, warm, resident) when dormant-until-invoked covers the demo
- Elaborate event/cursor/replay systems when simple polling would work for v0.3
- Encrypted artifact broker when file copy would work
- Two-sided policy enforcement for A2A when there are no real A2A partners yet

### Part 3: What to Keep and Strengthen

Identify the parts of the plan that are absolutely necessary for a secure, working product. Focus on:

- The core security promise: compromised agent cannot leak credentials or escape approved authority. Which blocks directly prove this? Which are essential?
- The developer experience: how easy is it to go from zero to running agent? What blocks make this better? What blocks make it worse?
- The wow factor: what will make someone post about AgentPaaS after trying it? What is the 10-minute experience?
- What can be enhanced within the existing scope to make the product more impressive without adding blocks?

### Part 4: Recommended Revised Block Sequence

Propose a trimmed block sequence for v0.3, v0.4, and v0.5. If blocks should be merged, split, or dropped, say so with clear reasoning. If a release can be smaller, say how much smaller.

Format as a table:

| Block | Release | Classification | Action | Reasoning (one line) |

Only include blocks that survive the cut. Add a separate list of dropped/deferred blocks with one-line reasoning each.

### Part 5: Risks and Gaps

Anything in the plan that worries you from an architecture perspective:
- Cross-block contract violations or assumptions that will not hold at integration
- Gate tests that may pass vacuously (the test runs but does not prove what it claims)
- Security claims that are not actually proven by the planned tests
- Missing pieces that are needed for a coherent product but are not in any block
- Sequencing risks: blocks that are ordered wrong or have hidden circular dependencies

Be honest and specific. Cite the exact spec language that worries you.

## Rules

1. READ-ONLY. You do not write files. You do not modify files. You do not run builds. You do not run tests. You do not execute code. Your only output is the audit report as your response.

2. RECOMMENDATIONS ONLY. Everything in your report is a recommendation to the founder. You are not making decisions. You are not changing the plan. You are providing analysis that the founder will use to make decisions.

3. Read everything before forming opinions. Do not skim. Do not skip sections. Do not read the first 50 lines of a 1000-line spec and form a view. Read the whole thing.

4. Cite specific files, decisions, and block numbers. Vague observations are useless. If you say "the plan is over-engineered," you must say which block, which spec language, and why.

5. The founder wants honesty, not validation. If something is useless, say so plainly. If something is brilliant, say so. Do not hedge. Do not soften. Do not pad with qualifiers.

6. Do not propose new features. The goal is to narrow scope, not broaden it.

7. Think about the audience: a developer sees a LinkedIn post about AgentPaaS, clicks through, reads the README, tries the quickstart. What do they need to experience in 10 minutes to be impressed? Everything beyond that is suspect.

8. Security is non-negotiable. If a cut weakens the core security promise, flag it and keep the block. The promise is: even if an agent is compromised, it cannot silently take credentials or communicate outside the authority its administrator approved.

9. Your report is the only output. Do not save it to a file. Do not print progress updates. Do not print "I am now reading block 28." Just read everything, then write the full report.

10. If you find that the plan is actually well-engineered and not over-engineered, say so with evidence. The founder suspects over-engineering but may be wrong. Your job is to find the truth, not to confirm the suspicion.
