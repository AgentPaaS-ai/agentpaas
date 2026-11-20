# Cloud login the right way

This step fails when Hermes tries to open the browser for you. Do it yourself.

1. In **your** terminal (not a Hermes tool that hangs):

```bash
agentpaas cloud login
```

2. Copy the printed URL.
3. Open it in the **same browser** that opened your claim link.
4. Click Approve.
5. Tell Hermes you are done. Hermes should run whoami only.

```bash
agentpaas cloud whoami
```

Never put `apc_…` tokens in chat. CI may use `cloud login --token-stdin` only.
