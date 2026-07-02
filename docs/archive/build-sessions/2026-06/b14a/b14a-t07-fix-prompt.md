# Task: Fix 14A-T07 Adversary Findings (3 MEDIUM)

You are on branch `feat/b14a-t07`. Fix the following adversary findings in `integrations/hermes-plugin/sanitizer.py`.

## Finding 1 (MEDIUM): Base64url encoding bypass

The base64 regex `r'[A-Za-z0-9+/]{16,}={0,2}'` does NOT match base64url encoding which uses `-` and `_` instead of `+` and `/`. An attacker can use base64url to bypass detection.

### Fix
Add a second regex pass for base64url. After the existing base64 decode block, add:

```python
# Base64url decode — URL-safe variant using - and _
for m in re.finditer(r'[A-Za-z0-9-_]{16,}={0,2}', text):
    chunk = m.group()
    try:
        decoded_bytes = base64.urlsafe_b64decode(chunk)
        decoded_str = decoded_bytes.decode('utf-8', errors='ignore')
        if decoded_str and all(32 <= ord(c) <= 126 or c in '\n\r\t' for c in decoded_str):
            decoded_parts.append(decoded_str)
    except (binascii.Error, ValueError):
        pass
```

## Finding 4 (MEDIUM): Non-ASCII/UTF-8 decode bypass

The printable filter `all(32 <= ord(c) <= 126 or c in '\n\r\t' for c in decoded_str)` rejects valid UTF-8 text containing non-ASCII characters. An attacker can embed injection directives using Unicode that survives UTF-8 decode but fails the ASCII filter.

### Fix
Relax the filter to accept any valid UTF-8 string (decoded with `errors='ignore'`), but add a length guard to avoid adding very long garbage strings. Replace the printable-ASCII filter in BOTH the base64 and hex decode paths with:

```python
# Accept any valid UTF-8 text that isn't empty or pure binary noise
if decoded_str and len(decoded_str) >= 4:
    # Reject if more than 30% of characters are non-printable
    printable_count = sum(1 for c in decoded_str if 32 <= ord(c) <= 126 or c in '\n\r\t')
    if printable_count / len(decoded_str) > 0.7:
        decoded_parts.append(decoded_str)
```

Apply this to ALL THREE decode paths (base64, base64url, hex).

## Finding 5 (MEDIUM): Missing security-relevant verbs

The narrowed directive pattern verb list is missing: purge, wipe, destroy, override, grant, revoke.

### Fix
Update the narrowed directive pattern at line ~42 to include the missing verbs:

```python
re.compile(
    r'(?i)\b(do not|don\'t|must|should|always|never)\b.{0,30}'
    r'\b(disable|enable|allow|deny|delete|remove|bypass|stop|kill|reveal|expose|'
    r'purge|wipe|destroy|override|grant|revoke)\b'
),
```

## Verification

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All 165 existing tests + verify no regressions. If any of the new T07 tests need updating due to the filter change, update them.

Also add one test for base64url detection:
```python
def test_base64url_encoded_injection_detected(self):
    import base64
    payload = b"disable policy gates"
    encoded = base64.urlsafe_b64encode(payload).decode()
    evidence = {"content": f"text {encoded} more"}
    result = sanitize_response(evidence)
    self.assertIn("_injection_warnings", result)
```

Add to `test_sanitizer_improvements.py`.

## Commit message
```
fix(14a-t07): adversary fixes — base64url decode, UTF-8 filter, missing verbs
```
