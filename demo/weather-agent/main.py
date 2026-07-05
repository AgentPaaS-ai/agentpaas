"""
Weather Agent — LLM-powered weather assistant.

This agent uses an LLM (via OpenRouter) for reasoning at two stages:
  1. Parse the user's natural-language question and extract the city name
  2. Analyze the weather data and produce a natural-language summary

The flow:
  User payload {"query": "What's the weather in Folsom?"}
    → LLM extracts city name ("Folsom")
    → Agent fetches weather from wttr.in API
    → LLM summarizes the raw weather data into a human-friendly answer

This demonstrates AgentPaaS governance over an LLM-powered agent:
  - LLM calls are budget-tracked and audited
  - HTTP egress is policy-controlled (only wttr.in allowed)
  - All actions recorded in the signed audit chain
"""
from agentpaas_sdk import agent


@agent.on_invoke
def invoke(payload):
    query = payload.get("query", "What's the weather in Folsom?")

    results = {}

    # Step 1: LLM reasoning — extract the city name from the user's question
    try:
        llm_resp = agent.llm(
            "Extract the city name from this question. "
            "Reply with ONLY the city name, nothing else. "
            "Question: " + query
        )
        city = llm_resp.get("text", "").strip()
        # Fallback if LLM returns empty or adds extra text
        if not city or len(city) > 50:
            city = "Folsom"
        results["city"] = city
    except Exception as e:
        results["llm_error"] = str(e)
        return {"scenario": "weather-agent", "results": results, "answer": None}

    # Step 2: Fetch weather data from wttr.in (policy-controlled egress)
    try:
        resp = agent.http("GET", f"https://wttr.in/{city}?format=j1")
        weather_data = resp.get("body", "")
        results["weather_fetched"] = True
    except Exception as e:
        results["weather_error"] = str(e)
        results["weather_fetched"] = False

        # If egress is denied, still let the LLM respond gracefully
        try:
            llm_resp = agent.llm(
                f"A user asked about the weather in {city}, but the weather API "
                f"is unavailable (error: {e}). Explain this to the user in one sentence."
            )
            return {
                "scenario": "weather-agent",
                "results": results,
                "answer": llm_resp.get("text", f"Could not fetch weather for {city}."),
            }
        except Exception as llm_err:
            return {
                "scenario": "weather-agent",
                "results": results,
                "answer": f"Could not fetch weather for {city} (error: {llm_err}).",
            }

    # Step 3: LLM reasoning — analyze weather data and produce a summary
    try:
        # Truncate weather data to stay within token budget
        weather_summary = weather_data[:2000] if weather_data else "No data"
        llm_resp = agent.llm(
            f"The user asked: '{query}'. "
            f"Here is the raw weather data for {city}:\n\n{weather_summary}\n\n"
            f"Give a concise, friendly 2-3 sentence weather summary answering the user's question. "
            f"Include temperature, conditions, and any relevant advice."
        )
        answer = llm_resp.get("text", "Unable to generate weather summary.")
        results["llm_summary"] = True
    except Exception as e:
        results["llm_summary_error"] = str(e)
        answer = f"Got weather data for {city} but couldn't generate a summary (error: {e})."

    return {"scenario": "weather-agent", "results": results, "answer": answer}
