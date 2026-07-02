"""Demo 2: Secret-brokered SaaS action.

Uses a brokered credential through the harness to make an authorized
API call. The secret value is never visible to agent code or logs —
the harness injects the credential header at the RPC layer.

The credential_id "crm-api-key" must be declared in the invoke
payload's credentials list. The harness matches it and adds the
Authorization header transparently.
"""
from agentpaas_sdk import agent


@agent.on_invoke
def invoke(payload):
    results = {}

    # Attempt an authorized SaaS call with a brokered credential.
    # The harness injects the credential header — agent code never
    # sees the secret value.
    try:
        resp = agent.http_with_credential(
            credential_id="crm-api-key",
            method="GET",
            url="https://api.salesforce.com/v2/account",
        )
        results["saas_call"] = {"status": "ok", "status_code": resp.get("status")}
    except Exception as e:
        results["saas_call"] = {"status": "denied", "error": str(e)}

    # Attempt without credential — should be denied by harness.
    try:
        resp = agent.http("GET", "https://api.salesforce.com/v2/account")
        results["uncredentialed_call"] = {"status": "ok"}
    except Exception as e:
        results["uncredentialed_call"] = {"status": "denied", "error": str(e)}

    return {"scenario": "secret-saas", "results": results}
