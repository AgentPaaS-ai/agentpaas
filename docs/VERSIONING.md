# Versioning policy — CLI, cloud, site, docs

**Goal:** every customer-facing surface is traceable to a release line. No silent drift between brew CLI, cloud API/UI, marketing site, and docs.

## Surfaces

| Surface | What customers see | Version marker |
|---------|-------------------|----------------|
| OSS CLI + daemon | `agentpaas version` | Semver tag `vX.Y.Z` + brew cask |
| Cloud API + dashboard | cloud.agentpaas.ai | Worker version ID + git SHA on agentpaas-cloud main; product label **M-series** (e.g. M10.6) |
| Marketing site | agentpaas.ai | Deploy timestamp / git SHA; docs under `/docs/` |
| Docs content | agentpaas.ai/docs + GitHub `docs/` | Docs **version folders** aligned to CLI major.minor (and cloud M-label when cloud-only) |

## Rules

1. **CLI point release** (e.g. 0.3.7): bump Makefile + version.go + tests; tag `vX.Y.Z`; goreleaser → brew; update release notes under `docs/customer/RELEASE-vX.Y.Z.md` **in the same tag**.
2. **Cloud-only UI/API** (no CLI bump): ship on cloud main; note in the planning repo's cloud current-state and add a dated entry under site `/docs/releases/` and OSS `docs/customer/`. Do **not** pretend brew changed.
3. **Docs always name the CLI version** they assume (`Requires CLI 0.3.7+`).
4. **Breaking API or dashboard IA** → bump cloud M-series label and open a docs version (keep previous docs readable).
5. **Never** put vendor infra brands (e.g. Cloudflare) in customer docs or UI copy.

## Recommended docs platform (decision)

See site page `/docs/platform/` and summary below.

**Choose: Docusaurus (OSS) for agentpaas.ai/docs long-term.**

| Option | Versioned docs | Search | Fit for AgentPaaS | Notes |
|--------|----------------|--------|-------------------|-------|
| **Docusaurus** | **First-class** (`docs/versioned_docs`) | Algolia or local | **Best** for multi-version CLI + product | React; used by many API products |
| Starlight (Astro) | Manual / community | Pagefind built-in | Excellent perf on CF Pages | Faster builds; versioning less turnkey |
| VitePress | Manual folders | Built-in | Good if Vue shop | We are not Vue-primary |
| MkDocs Material | mike plugin | Built-in | Fine for pure Markdown | Weaker multi-product IA |
| Nextra | Weak | — | Skip | Next.js weight without versioning win |
| GitBook / Mintlify | Hosted | Yes | Fast but not fully OSS-controlled | Avoid for core product truth |

**Near term (now):** keep static HTML under `site/docs/` + GitHub Markdown (this release).  
**Next platform cut:** stand up Docusaurus at `docs.agentpaas.ai` or `agentpaas.ai/docs` with versions `0.3`, `0.4`, … mirrored from `oss/docs/`. Cloud M-series release notes as a second sidebar product.

## Release checklist (docs)

- [ ] `docs/customer/RELEASE-vX.Y.Z.md` written
- [ ] GitHub release body updated (`gh release edit`)
- [ ] Site `/docs/` hub + release page published
- [ ] Trial spine commands verified against that CLI version
- [ ] Brew `agentpaas version` matches tag
