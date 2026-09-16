"""LangGraph weather-class golden. LLM via harness OpenAI loopback."""
import os
from typing import TypedDict

from agentpaas_sdk import agent
from langgraph.graph import END, START, StateGraph
from openai import OpenAI


class WeatherState(TypedDict):
    query: str
    city: str
    weather_data: str
    answer: str
    results: dict
    llm_error: str


def _client() -> OpenAI:
    return OpenAI(
        base_url=os.environ["OPENAI_BASE_URL"],
        api_key=os.environ["OPENAI_API_KEY"],
    )


def _chat(prompt: str) -> str:
    model = os.environ.get("OPENAI_MODEL", "gpt-4o")
    resp = _client().chat.completions.create(
        model=model,
        messages=[{"role": "user", "content": prompt}],
    )
    return (resp.choices[0].message.content or "").strip()


def extract_city(state: WeatherState) -> WeatherState:
    results = dict(state.get("results") or {})
    query = state.get("query") or ""
    try:
        city = _chat(
            "Extract the city name from this question. "
            "Reply with ONLY the city name, nothing else. Question: " + query
        )
        if not city or len(city) > 50:
            city = "Folsom"
        results["city"] = city
        return {**state, "city": city, "results": results}
    except Exception as e:
        results["llm_error"] = str(e)
        return {**state, "results": results, "llm_error": str(e), "answer": ""}


def fetch_weather(state: WeatherState) -> WeatherState:
    if state.get("llm_error"):
        return state
    results = dict(state.get("results") or {})
    city = state.get("city") or "Folsom"
    weather_data = ""
    try:
        resp = agent.http("GET", f"https://wttr.in/{city}?format=j1")
        weather_data = resp.get("body", "") or ""
        results["weather_fetched"] = True
    except Exception as e:
        results["weather_error"] = str(e)
        results["weather_fetched"] = False
    return {**state, "weather_data": weather_data, "results": results}


def summarize(state: WeatherState) -> WeatherState:
    results = dict(state.get("results") or {})
    if state.get("llm_error"):
        return {**state, "answer": "", "results": results}
    query = state.get("query") or ""
    city = state.get("city") or "Folsom"
    weather_summary = (state.get("weather_data") or "")[:2000] or "No data"
    try:
        answer = _chat(
            f"The user asked: '{query}'. "
            f"Here is the raw weather data for {city}:\n\n{weather_summary}\n\n"
            "Give a concise, friendly 2-3 sentence weather summary answering "
            "the user's question. Include temperature, conditions, and any relevant advice."
        )
        results["llm_summary"] = True
    except Exception as e:
        results["llm_summary_error"] = str(e)
        answer = (
            f"Got weather data for {city} but couldn't generate a summary (error: {e})."
        )
    return {**state, "answer": answer, "results": results}


def _graph():
    g = StateGraph(WeatherState)
    g.add_node("extract_city", extract_city)
    g.add_node("fetch_weather", fetch_weather)
    g.add_node("summarize", summarize)
    g.add_edge(START, "extract_city")
    g.add_edge("extract_city", "fetch_weather")
    g.add_edge("fetch_weather", "summarize")
    g.add_edge("summarize", END)
    return g.compile()


@agent.on_invoke
def invoke(payload):
    query = payload.get("query", "What's the weather in Folsom?")
    app = _graph()
    out = app.invoke(
        {
            "query": query,
            "city": "",
            "weather_data": "",
            "answer": "",
            "results": {},
            "llm_error": "",
        }
    )
    answer = out.get("answer") or None
    results = out.get("results") or {}
    if out.get("llm_error") and not answer:
        return {
            "scenario": "langgraph-weather-agent",
            "results": results,
            "answer": None,
            "final_output": None,
        }
    return {
        "scenario": "langgraph-weather-agent",
        "results": results,
        "answer": answer,
        "final_output": answer,
    }
