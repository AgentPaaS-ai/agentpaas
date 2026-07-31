# M7.5 task breakdown

| Task | Title | Repo | Status |
|------|-------|------|--------|
| T01 | Artifacts + signed R2 URLs (SC1) | cloud | DONE 557e462 |
| T02 | Completion webhook HMAC + SSRF (SC2) | cloud | DONE ca986cb |
| T03 | Delivery channel declared output only (SC3) | cloud | DONE 602b1bb |
| T04 | `cloud result` / `cloud logs` polish | cloud + OSS | DONE (OSS merge) |
| T05 | Always-on prod API + deploy re-bind | cloud + docs | DONE 3b39bc3 |
| T06 | Golden-path customer runbook | OSS docs | DONE |
| T07 | Invoke-token Keychain/`~/.agentpaas` store | OSS | DONE (with T04) |
| T08 | D105 rate-limit matrix (SC4) | cloud | DONE 1a7e257 |
| T09 | Quota UX (reset, upgrade CTA, trial expiry path) | cloud + OSS | DONE |
| T10 | Secrets posture: label "preview vault" (D114) | cloud + docs | DONE 496a893 |
| T11 | Cold-onboard external gate (founder+external) | human | PENDING — D113 non-deferrable |
| T12 | Live CF soak $ cap | human+cloud | PENDING |
| ADV | SC1–SC5 adversary | cloud | after T01–T08 |
| ARCH | Thinker architecture review | read-only | block-end |
| VER | Verifier 8 segments | cloud+OSS | block-end |

Sequencing: T01 → T02 → T03 (output loop). T04 can trail T01.
T08 independent of T02/T03 after T01. T06 needs T01–T05 for truth.
T11/T12 are human gates after eng green.
