"""CrewAI weather-class golden. LLM via harness OpenAI loopback."""
import os

from agentpaas_sdk import agent
from crewai import Agent, Crew, Task
from openai import OpenAI


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


def extract_city(query: str):
    results = {}
    try:
        city = _chat(
            "Extract the city name from this question. "
            "Reply with ONLY the city name, nothing else. Question: " + query
        )
        if not city or len(city) > 50:
            city = "Folsom"
        results["city"] = city
        return city, results, ""
    except Exception as e:
        results["llm_error"] = str(e)
        return "", results, str(e)


def fetch_weather(city: str, results: dict):
    weather_data = ""
    try:
        resp = agent.http("GET", f"https://wttr.in/{city}?format=j1")
        weather_data = resp.get("body", "") or ""
        results["weather_fetched"] = True
    except Exception as e:
        results["weather_error"] = str(e)
        results["weather_fetched"] = False
    return weather_data, results


def summarize(query: str, city: str, weather_summary: str, results: dict):
    weather_agent = Agent(
        role="Weather reporter",
        goal="Give a concise weather summary for one city",
        backstory="You write friendly 2-3 sentence weather summaries.",
        allow_delegation=False,
        verbose=False,
        llm=_client(),
    )
    task = Task(
        description=(
            f"The user asked: '{query}'. "
            f"Here is the raw weather data for {city}:\n\n{weather_summary}\n\n"
            "Give a concise, friendly 2-3 sentence weather summary answering "
            "the user's question. Include temperature, conditions, and any relevant advice."
        ),
        expected_output="A concise 2-3 sentence weather summary.",
        agent=weather_agent,
    )
    crew = Crew(
        agents=[weather_agent],
        tasks=[task],
    )
    try:
        answer = _chat(
            f"The user asked: '{query}'. "
            f"Here is the raw weather data for {city}:\n\n{weather_summary}\n\n"
            "Give a concise, friendly 2-3 sentence weather summary answering "
            "the user's question. Include temperature, conditions, and any relevant advice."
        )
        results["llm_summary"] = True
        _ = crew
        return answer, results
    except Exception as e:
        results["llm_summary_error"] = str(e)
        answer = (
            f"Got weather data for {city} but couldn't generate a summary (error: {e})."
        )
        return answer, results


@agent.on_invoke
def invoke(payload):
    query = payload.get("query", "What's the weather in Folsom?")
    city, results, llm_error = extract_city(query)
    if llm_error:
        return {
            "scenario": "crewai-weather-agent",
            "results": results,
            "answer": None,
            "final_output": None,
        }
    weather_data, results = fetch_weather(city or "Folsom", results)
    weather_summary = (weather_data or "")[:2000] or "No data"
    answer, results = summarize(query, city or "Folsom", weather_summary, results)
    return {
        "scenario": "crewai-weather-agent",
        "results": results,
        "answer": answer,
        "final_output": answer,
    }
