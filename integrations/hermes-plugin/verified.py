"""Judge whether MCP/Jira prove evidence may stamp verified.

BUG-JIRA-MCP-FALSE-VERIFIED: empty search, 0 projects, and 404 on a
fake/unnamed key cannot close verified. MCP prove must be tools/call,
not trigger invoke of an on_invoke dispatcher.
"""

import re

_FAKE_KEY = re.compile(r"^(NOSUCH|FOO|FAKE)[-_]\d+$", re.IGNORECASE)

_SEARCH_TOOLS = frozenset({"search", "search_issues", "search_jql", "jql_search"})
_PROJECT_TOOLS = frozenset({"list_projects", "get_projects", "projects"})
_GET_TOOLS = frozenset({"get_issue", "get_comments", "get_issue_comments"})


def _unique(items):
    seen = set()
    ordered = []
    for item in items:
        if item not in seen:
            seen.add(item)
            ordered.append(item)
    return ordered


def _http_status(call):
    for key in ("http_status", "status", "status_code"):
        value = call.get(key)
        if value is None:
            continue
        try:
            return int(value)
        except (TypeError, ValueError):
            continue
    return None


def _as_list(value):
    if isinstance(value, list):
        return value
    return None


def _count(call, list_keys):
    for key in list_keys:
        items = _as_list(call.get(key))
        if items is not None:
            return len(items)
    for key in ("count", "total", "total_count"):
        value = call.get(key)
        if value is None:
            continue
        try:
            return int(value)
        except (TypeError, ValueError):
            continue
    return None


def _issue_key(call):
    for key in ("key", "issue_key", "id"):
        value = call.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def _is_fake_key(key, user_named_keys):
    if not key:
        return True
    if _FAKE_KEY.match(key):
        return True
    named = {str(k).strip().upper() for k in (user_named_keys or []) if k}
    if named and key.upper() not in named:
        return True
    return False


def evaluate_verified_closeout(evidence):
    """Return {verified: bool, reasons: [str]} for MCP/Jira prove evidence.

    Fail closed. A closeout may stamp verified only when prove_method is
    tools/call and none of the anti-fabrication reasons fire.
    """
    evidence = evidence or {}
    reasons = []
    prove_method = str(evidence.get("prove_method") or "").strip()
    if prove_method != "tools/call":
        reasons.append("prove_not_tools_call")

    calls = evidence.get("calls")
    if not isinstance(calls, list) or not calls:
        reasons.append("no_evidence")
        return {"verified": False, "reasons": _unique(reasons)}

    user_named_keys = evidence.get("user_named_keys") or []
    if not isinstance(user_named_keys, list):
        user_named_keys = []

    for call in calls:
        if not isinstance(call, dict):
            continue
        tool = str(call.get("tool") or call.get("name") or "").strip()
        status = _http_status(call)

        if tool in _PROJECT_TOOLS:
            count = _count(call, ("projects", "values", "items"))
            if count == 0:
                reasons.append("zero_projects")

        if tool in _SEARCH_TOOLS:
            count = _count(call, ("issues", "values", "items", "results"))
            if count == 0:
                reasons.append("empty_search")

        if tool in _GET_TOOLS and status == 404:
            key = _issue_key(call)
            # NOSUCH-1 / FOO-1 / any key the user did not name, and 404
            # on a named live id, cannot stamp verified.
            if _is_fake_key(key, user_named_keys) or status == 404:
                reasons.append("fake_key_404")

    reasons = _unique(reasons)
    return {"verified": not reasons, "reasons": reasons}


def stamp_verified(evidence):
    """Stamp verified only when the closeout judge accepts the evidence."""
    result = evaluate_verified_closeout(evidence)
    return {
        "verified": bool(result.get("verified")),
        "stamped": bool(result.get("verified")),
        "reasons": list(result.get("reasons") or []),
    }
