# BUG-038 — Install fingerprint confirm rejects spaced last-8 (display vs input mismatch)

**Status:** OPEN  
**Severity:** P2 (UX — blocks first-time bundle install TOFU)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (receiver `agentpaas install`)  
**Build:** CLI 0.3.0-dev commit ed22b0f  
**Founder:** hit during install; type-without-spaces works; product should accept display form.

## Symptom

```
Publisher fingerprint: 2b0c adb0 c5ec 2de8 a89d 4f8c e491 46dd 88a3 6f36 f0a5 feb6 9de1 4825 2bc8 ae1e

Verify this fingerprint with the sender over another channel before continuing.
Type the LAST 8 characters of the fingerprint to confirm: 2bc8 ae1e
The typed value does not match. Verify the fingerprint carefully.
```

User typed the last group pair **exactly as displayed** (`2bc8 ae1e`). Rejected.

## Root cause (code)

`internal/install/trust.go`:

- Display uses spaced fingerprint (`displayFP`).
- Expected confirm value is `last8Hex(fp)` on the **normalized** hex string (no spaces) → e.g. `2bc8ae1e`.
- User response is only `strings.ToLower(strings.TrimSpace(response))` — **internal spaces are not stripped** before compare (`resolveTOFUInteractive` ~274–276).

So the UI invites copying the last displayed chunk(s) with a space; the matcher wants 8 contiguous hex digits.

## Expected

Any of:

1. Accept input with spaces/colons removed (`2bc8 ae1e` → `2bc8ae1e`).
2. Prompt text shows the exact required token: `Type: 2bc8ae1e` (or show both display and “type without spaces”).
3. Prefer: normalize with existing `trust.NormalizeFingerprint` (or strip non-hex) then compare last 8.

## Actual

Spaced last-8 from display fails; continuous `2bc8ae1e` succeeds (tests use continuous hex only).

## Workaround (operators)

Type the last 8 hex digits **with no space**: `2bc8ae1e`  
(not `2bc8 ae1e`).

## Fix later

- Normalize confirm input the same way as fingerprints before compare.
- Add unit test: prompt response `"2bc8 ae1e"` pins successfully for fp ending in `…2bc8ae1e`.
- Optional: prompt example uses the spaced display form so copy-paste works.

## Related

- BUG-037 install PATH discovery  
- TOFU last-8 design in `internal/install/trust.go` / adversary last-8 tests (continuous only)
