"""Response sanitizer — enforces the trusted/untrusted field boundary.

This module provides functions that:
1. Classify response fields as trusted control or untrusted evidence
2. Sanitize untrusted evidence (strip control-channel patterns)
3. Detect prompt-injection attempts in evidence fields
4. Ensure trusted control fields are never sourced from untrusted evidence
"""
import json
import re
import urllib.parse

# Patterns that indicate prompt-injection attempts in untrusted evidence
# These are patterns that, if found in evidence fields, should be flagged
# but NOT strip legitimate diagnostic content.
_INJECTION_PATTERNS = [
    # Attempts to issue commands or override instructions
    re.compile(r'(?i)\b(ignore|disregard|forget)\s+(all\s+)?(previous|prior|above)\b'),
    re.compile(r'(?i)\b(ignore|disregard|forget)\s+policy\b'),
    re.compile(r'(?i)\b(system|admin|root)\s*(prompt|instruction|command)\b'),
    re.compile(
        r'(?i)\b(disable|bypass|turn\s+off)\s+(?:all\s+)?'
        r'(polic(?:y|ies)|gates?|audits?|firewalls?|sandboxes?)\b'
    ),
    re.compile(
        r'(?i)\b(reveal|show|print|expose|leak)\s+(?:the\s+)?'
        r'(?:api\s+)?(secret|token|key|credential|password)\b'
    ),
    re.compile(
        r'(?i)\b(delete|purge|wipe|destroy)\s+(?:all\s+)?'
        r'(audit|log|evidence|trail)s?\b'
    ),
    re.compile(
        r'(?i)\b(stop|kill|terminate)\s+(?:all\s+)?'
        r'(unrelated|other)\s+runs?\b'
    ),
    re.compile(r'(?i)\b(increase|raise|remove)\s+(budget|limit|cap|quota)\b'),
    re.compile(r'(?i)\ballow\s+all\s+egress\b'),
    # Instruction-like directives in evidence context
    re.compile(r'(?i)\b(do not|don\'t|must|should|always|never)\b'),
    # JSON/YAML injection attempts (fake control fields in evidence)
    re.compile(r'"(error_category|next_action|requires_confirmation|policy_digest)"\s*:'),
    # Abbreviations
    re.compile(r'(?i)\brm\s+(audit|log|evidence|trail)'),
    re.compile(r'(?i)\bkill\s+(all|unrelated|other|active)\s+runs?'),
    # Indirect phrasing
    re.compile(
        r'(?i)\b(it\s+would\s+be|please|you\s+should|must|need\s+to)\b.{0,30}'
        r'\b(disable[ds]?|enable[ds]?|allow(?:ed)?|deny|denied|stop(?:ped)?|'
        r'delete[ds]?|reveal(?:ed)?|bypass(?:ed)?)\b'
    ),
    re.compile(
        r'(?i)\b(policy|gate|audit|sandbox)\s+(should\s+be|must\s+be|were)\s+'
        r'(disabled|removed|off)\b'
    ),
]

_EVIDENCE_LIST_FIELDS = ("redacted_excerpts", "evidence_refs")

# Fields where injection patterns should be flagged
_UNTRUSTED_CONTEXTS = ("content", "detail", "raw_output_truncated", "summary", "root_cause")

_CONTROL_FIELD_NAMES = (
    "error_category", "next_action", "requires_confirmation",
    "confirmation_id", "risk_level", "policy_digest",
    "blocking_rule_id", "schema_version",
)


def _decode_evidence_text(text):
    """Decode common encoding schemes before injection detection."""
    decoded = text
    # URL decode (handle %64, %xx)
    try:
        url_decoded = urllib.parse.unquote(decoded)
        if url_decoded != decoded:
            decoded = url_decoded + " " + decoded  # scan both
    except Exception:
        pass
    return decoded


def _detect_structural_injection(text):
    """Detect JSON-like content in evidence that mimics control fields."""
    if not text or not isinstance(text, str):
        return []
    findings = []
    # Look for JSON-like patterns containing control field names
    for field_name in _CONTROL_FIELD_NAMES:
        pattern = re.compile(rf'"({field_name})"\s*:\s*"([^"]*)"')
        for m in pattern.finditer(text):
            findings.append((
                f"structural_injection:{field_name}",
                m.group()
            ))
    return findings


def classify_field(field_name):
    """Classify a response field as 'trusted' or 'untrusted'.
    Returns 'trusted', 'untrusted', or 'complex' (for nested containers).
    """
    from . import contracts  # lazy import to avoid circular dependency

    if field_name in contracts.TRUSTED_CONTROL_FIELDS:
        return "trusted"
    if field_name in contracts.UNTRUSTED_EVIDENCE_FIELDS:
        return "untrusted"
    # Complex container fields (issues, events, confirmation, excerpts)
    return "complex"


def detect_injection_in_evidence(text):
    """Detect prompt-injection patterns in an untrusted evidence string.
    Returns a list of (pattern_description, matched_text) tuples.
    Empty list = no injection detected.
    """
    if not text or not isinstance(text, str):
        return []
    decoded = _decode_evidence_text(text)
    findings = []
    for pattern in _INJECTION_PATTERNS:
        for source_text in (decoded, text):
            for m in pattern.finditer(source_text):
                findings.append((pattern.pattern, m.group()))
    return findings


def sanitize_response(response_dict):
    """Sanitize a CLI response before returning to the Hermes model.

    1. Verifies trusted control fields are present and well-typed.
    2. Scans untrusted evidence fields for injection patterns.
    3. Adds an '_injection_warnings' field if patterns are detected.
    4. NEVER strips evidence (the model needs diagnostic content) — it flags it.

    Returns the sanitized response dict.
    """
    if not isinstance(response_dict, dict):
        return response_dict

    result = dict(response_dict)
    warnings = []

    def _scan_text(field_path, text):
        findings = detect_injection_in_evidence(text)
        if findings:
            warnings.extend([
                {"field": field_path, "pattern": p, "match": m}
                for p, m in findings
            ])
        struct_findings = _detect_structural_injection(text)
        if struct_findings:
            warnings.extend([
                {"field": field_path, "pattern": p, "match": m, "severity": "high"}
                for p, m in struct_findings
            ])

    def _scan_evidence_list(field_name, items):
        for i, item in enumerate(items):
            if isinstance(item, dict):
                # Scan ALL string values in the item, not just specific subfields
                for sub_field, sub_val in item.items():
                    if isinstance(sub_val, str):
                        _scan_text(f"{field_name}[{i}].{sub_field}", sub_val)

    # Scan untrusted evidence fields for injection patterns
    for field in _UNTRUSTED_CONTEXTS:
        val = result.get(field)
        if isinstance(val, str):
            _scan_text(field, val)
        elif isinstance(val, list):
            _scan_evidence_list(field, val)

    for field in _EVIDENCE_LIST_FIELDS:
        val = result.get(field)
        if isinstance(val, list):
            _scan_evidence_list(field, val)

    if warnings:
        result["_injection_warnings"] = warnings
        # Mark the specific evidence fields that contain hostile content
        flagged_fields = set(w["field"].split("[")[0].split(".")[0] for w in warnings)
        result["_untrusted_fields"] = sorted(flagged_fields)
        # Ensure trusted control fields are NOT overridden by evidence content
        # (This is the key boundary: even if evidence says "error_category: X",
        #  the trusted error_category from the CLI response is preserved)

    return result


def validate_trusted_field_integrity(response_dict, original_cli_response):
    """Verify that trusted control fields were NOT sourced from untrusted evidence.

    Compares the response's trusted fields against the original CLI output.
    If a trusted field value appears in an untrusted evidence field, flag it.

    Returns a list of integrity violations (empty = clean).
    """
    if not isinstance(response_dict, dict) or not isinstance(original_cli_response, dict):
        return []

    violations = []
    from . import contracts

    # Collect all untrusted evidence text
    evidence_text = ""
    for field in contracts.UNTRUSTED_EVIDENCE_FIELDS:
        val = original_cli_response.get(field)
        if isinstance(val, str):
            evidence_text += " " + val
        elif isinstance(val, list):
            for item in val:
                if isinstance(item, dict):
                    for v in item.values():
                        if isinstance(v, str):
                            evidence_text += " " + v

    # Check if any trusted field value appears in evidence text
    # (This would indicate the trusted field was sourced FROM evidence)
    for field in contracts.TRUSTED_CONTROL_FIELDS:
        resp_val = response_dict.get(field)
        orig_val = original_cli_response.get(field)
        if resp_val != orig_val:
            violations.append({
                "field": field,
                "violation": "trusted field modified between CLI and response",
                "cli_value": orig_val,
                "response_value": resp_val,
            })

    return violations