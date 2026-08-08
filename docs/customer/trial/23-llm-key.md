# Your LLM key

For the first trial, use **OpenRouter** (only cold path fully proven).

```bash
agentpaas secret add openrouter-key
# paste key at the prompt (stdin). Never put the key on the command line or in chat.
agentpaas secret list
```

Cheap OpenRouter defaults: `deepseek/deepseek-chat` or `openai/gpt-4o-mini`.  
Do not use stale model ids from old docs.

Cloud: after deploy,

```bash
agentpaas cloud secrets push openrouter-key
agentpaas cloud secrets bind <deployment> openrouter-key --as bearer --host openrouter.ai
```

Rotate by pushing the **same label** again. Delete is blocked while deployments are bound (409) unless forced.
