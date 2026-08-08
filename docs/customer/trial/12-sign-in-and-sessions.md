# Sign in and sessions

- Browser session cookie powers the dashboard.
- Log out from the dashboard header.
- CLI uses a separate tenant token in Keychain (`agentpaas-cloud-api-token`) from **Approve CLI Login**.
- Tokens tab may show **bootstrap** (`default`) and **cli-login**. Day to day CLI uses cli-login.

If whoami says not logged in, run cloud login again (see [Cloud login](41-cloud-login.md)).
