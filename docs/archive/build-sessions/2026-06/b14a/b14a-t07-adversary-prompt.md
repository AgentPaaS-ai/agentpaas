# Adversary Review: 14A-T07 Sanitizer Improvements

You are reviewing security code changes. Review the following changes on branch `feat/b14a-t07` in the agentpaas repo at /Users/pms88/projects/agentpaas.

## What was changed
File: `integrations/hermes-plugin/sanitizer.py`

1. `_decode_evidence_text()` now decodes base64 (≥16 char substrings) and hex (≥20 char substrings) in addition to URL encoding
2. Directive injection pattern (line 40) narrowed: was `re.compile(r'(?i)\b(do not|don\'t|must|should|always|never)\b')`, now requires these words to be followed within 30 chars by a security-relevant verb (disable, enable, allow, deny, delete, remove, bypass, stop, kill, reveal, expose)
3. New `_detect_yaml_injection()` function detects YAML-style control field injection (e.g., `next_action: value` outside JSON)
4. YAML injection detection called alongside structural injection in `_scan_text()` in `sanitize_response()`

## Review checklist

1. **Base64 false negatives**: Can an attacker bypass by using base64url encoding (using - instead of + and _ instead of /)? The regex `[A-Za-z0-9+/]{16,}={0,2}` does NOT match base64url. Is this a gap?

2. **Hex decode false positives**: The hex regex `[0-9a-fA-F]{20,}` matches normal hex hashes like SHA-1 (40 chars). Will this cause unnecessary decode attempts? Are the decoded bytes likely to be non-printable and thus filtered, or could hash bytes accidentally decode to something that triggers injection patterns?

3. **YAML injection double-reporting**: Does `_detect_yaml_injection()` also match inside JSON strings? E.g., if evidence contains `{"next_action": "safe"}`, both `_detect_structural_injection` and `_detect_yaml_injection` would match. Is this a problem?

4. **Printable ASCII filter bypass**: The filter `all(32 <= ord(c) <= 126 or c in '\n\r\t' for c in decoded_str)` rejects non-printable decoded output. But what about Unicode? Could an attacker use a base64 string that decodes to valid UTF-8 with non-ASCII chars that still contains injection patterns?

5. **Narrowed pattern completeness**: The narrowed directive pattern verbs are: disable, enable, allow, deny, delete, remove, bypass, stop, kill, reveal, expose. Are there security-relevant verbs missing? (e.g., "purge", "wipe", "destroy", "escalate", "grant", "revoke", "override", "inject", "tamper")

6. **ReDoS risk**: The base64 regex `[A-Za-z0-9+/]{16,}={0,2}` — is there any catastrophic backtracking risk? What about the hex regex `[0-9a-fA-F]{20,}`?

7. **HOME/env var in new code**: Does the new code use `$HOME` or `os.path.expanduser("~")` anywhere? (It shouldn't — this is a sanitizer, not a path resolver, but check.)

Run `git diff main` on branch `feat/b14a-t07` to see all changes. Report findings as:
```
FINDING N: [severity] [title]
Description: ...
Impact: ...
Recommendation: ...
```
