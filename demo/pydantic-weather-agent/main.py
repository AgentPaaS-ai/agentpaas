"""Pydantic AI weather-class golden. LLM via harness OpenAI loopback."""
import os
from agentpaas_sdk import agent
from pydantic_ai import Agent


def _pydantic_agent():
    model = os.environ.get("OPENAI_MODEL", "gpt-4o")
    # OPENAI_BASE_URL and OPENAI_API_KEY are injected by the harness loopback.
    _ = os.environ["OPENAI_BASE_URL"]
    _ = os.environ["OPENAI_API_KEY"]
    return Agent(f"openai:{model}")


@agent.on_invoke
def invoke(payload):
    query = payload.get("query", "What's the weather in Folsom?")
    results = {}
    llm = _pydantic_agent()
    try:
        result = llm.run_sync(
            "Extract the city name from this question. "
            "Reply with ONLY the city name, nothing else. Question: " + query
        )
        city = str(getattr(result, "output", None) or getattr(result, "data", "")).strip()
        if not city or len(city) > 50:
            city = "Folsom"
        results["city"] = city
    except Exception as e:
        results["llm_error"] = str(e)
        return {
            "scenario": "pydantic-weather-agent",
            "results": results,
            "answer": None,
            "final_output": None,
        }

    weather_data = ""
    try:
        resp = agent.http("GET", f"https://wttr.in/{city}?format=j1")
        weather_data = resp.get("body", "") or ""
        results["weather_fetched"] = True
    except Exception as e:
        results["weather_error"] = str(e)
        results["weather_fetched"] = False

    weather_summary = weather_data[:2000] if weather_data else "No data"
    try:
        result = llm.run_sync(
            f"The user asked: '{query}'. "
            f"Here is the raw weather data for {city}:\n\n{weather_summary}\n\n"
            "Give a concise, friendly 2-3 sentence weather summary answering "
            "the user's question. Include temperature, conditions, and any relevant advice."
        )
        answer = str(getattr(result, "output", None) or getattr(result, "data", "") or "")
        results["llm_summary"] = True
    except Exception as e:
        results["llm_summary_error"] = str(e)
        answer = f"Got weather data for {city} but couldn't generate a summary (error: {e})."

    return {
        "scenario": "pydantic-weather-agent",
        "results": results,
        "answer": answer,
        "final_output": answer,
    }
