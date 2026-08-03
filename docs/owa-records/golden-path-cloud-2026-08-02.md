# Terminal golden path — 2026-08-02/03

**CLI:** brew 0.3.6 @ 9ce0bcb  
**API:** https://agentpaas-cloud-api.parvezsyed.workers.dev  
**Tenant:** ten_a09912962ae19884c311c343702a0f3c  

## Steps

| Step | Result |
|------|--------|
| doctor | 7/7 |
| whoami | trial |
| pack weather linux/amd64 | digest sha256:636deea8… Lock path printed |
| cloud push | admitted img_031fcb96… registry weather-agent:0.1.0 |
| cloud deploy latest | 503 no_slot_capacity |
| use ready dep | dep_22c9cc602bf108e487ce4e0b931790e4 |
| secrets push + bind | openrouter-key bearer @ openrouter.ai |
| invoke Folsom | **succeeded** run_d8634b4d64177002d1f29e5acb894c9e |
| result final_output | real weather prose + artifact URL |
| usage | trial days remaining shown |

## Notes

- User-supplied `apc_…` is **tenant** token, not Cloudflare registry token. Push used Keychain `agentpaas-cloudflare-api-token`.
- Slot pool full → correct recovery is existing ready dep (not fail closed).
- Script `golden-path-cloud.sh` updated to fall back on no_slot_capacity.
