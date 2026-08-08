# Cloud pull (edit loop)

Requires CLI **0.3.7+**.

```bash
agentpaas cloud pull weather-agent --out ./weather-from-cloud
cd ./weather-from-cloud
# edit agent.yaml / main.py
# bump version in agent.yaml
agentpaas pack --target linux/amd64
agentpaas cloud push --lock ~/.agentpaas/state/agents/weather-agent/agent.lock
agentpaas cloud deploy latest
```

Pull is a metadata-first MVP. Prefer keeping a local project as source of truth when you can.
