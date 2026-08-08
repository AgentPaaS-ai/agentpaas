# The 30-minute path

End-to-end trial map. Detail pages are linked from each step.

1. Open your **claim link** (email from founder) → set password → optional Google link  
   → [Claim](11-claim-your-trial.md) · [Sign in](12-sign-in-and-sessions.md)
2. Install Hermes and the AgentPaaS plugin  
   → [Install](21-install-macos.md) · [Hermes](22-hermes-plugin.md)
3. In Hermes paste:  
   `Install from https://github.com/AgentPaaS-ai/agentpaas`
4. Paste:  
   `Build a weather agent that uses an LLM, and responds in a friendly demeanour`  
   Use **OpenRouter** + a cheap model. Store key via terminal stdin.  
   → [LLM key](23-llm-key.md)
5. Local check: `Show me lineage and audits`
6. Cloud: `Make it run in the AgentPaaS cloud`  
   When asked to log in, **you** run `agentpaas cloud login` in your own terminal.  
   → [Cloud login](41-cloud-login.md)
7. Open https://cloud.agentpaas.ai — Agents, Deployments, Runs, Cron, Usage  
   → [Dashboard](51-dashboard-tour.md)
8. Optional schedule:  
   `agentpaas cloud cron set <deployment> --expr every_5m`  
   → [Cron](45-cloud-cron.md)

If stuck: [Troubleshooting](60-troubleshooting.md)
