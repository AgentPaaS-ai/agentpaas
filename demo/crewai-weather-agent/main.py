"""CrewAI weather-class golden. LLM via harness OpenAI loopback."""
import os

from agentpaas_sdk import agent
from crewai import Agent, Crew, LLM, Task


def _llm() -> LLM:
    return LLM(
        model=os.environ.get("OPENAI_MODEL", "gpt-4o"),
        base_url=os.environ["OPENAI_BASE_URL"],
        api_key=os.environ["OPENAI_API_KEY"],
    )


@agent.on_invoke
def invoke(payload):
    query = payload.get("query", "What's the weather in Folsom?")
    results = {}
    city = "Folsom"
    if " in " in query:
        tail = query.split(" in ", 1)[1]
        for stop in ["?", ".", "!", ","]:
            tail = tail.split(stop, 1)[0]
        tail = tail.strip()
        if tail and len(tail) <= 50:
            city = tail
    results["city"] = city

    weather_data = ""
    try:
        resp = agent.http("GET", f"https://wttr.in/{city}?format=j1")
        weather_data = resp.get("body", "") or ""
        results["weather_fetched"] = True
    except Exception as e:
        results["weather_error"] = str(e)
        results["weather_fetched"] = False

    weather_summary = weather_data[:2000] if weather_data else "No data"
    weather_agent = Agent(
        role="Weather reporter",
        goal="Give a concise weather summary for one city",
        backstory="You write friendly 2-3 sentence weather summaries.",
        allow_delegation=False,
        verbose=False,
        llm=_llm(),
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
        answer = str(crew.kickoff()).strip()
        results["llm_summary"] = True
    except Exception as e:
        results["llm_summary_error"] = str(e)
        answer = f"Got weather data for {city} but couldn't generate a summary (error: {e})."

    return {
        "scenario": "crewai-weather-agent",
        "results": results,
        "answer": answer,
        "final_output": answer,
    }
