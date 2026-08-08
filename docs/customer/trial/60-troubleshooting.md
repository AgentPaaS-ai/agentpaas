# Troubleshooting (trial)

## 6.1 Claim / login

| Symptom | Fix |
|---------|-----|
| "Open your claim link first" | You need a founder claim URL before browser login |
| Hermes stuck on cloud login | Stop. Run `agentpaas cloud login` yourself; open URL in claim browser |
| Wrong browser | Approve CLI login in the **same** browser as claim |

## 6.2 Install / Gatekeeper

| Symptom | Fix |
|---------|-----|
| macOS malware warning | `xattr -cr` on brew binaries, or right-click Open |
| Tools missing in Hermes | `/quit` and reopen after plugin install |

## 6.3 Invoke empty / no weather

| Symptom | Fix |
|---------|-----|
| succeeded but empty output | Check `cloud secrets bindings <dep>`; bind openrouter-key to openrouter.ai |
| secret delete fails 409 | Expected if deployments bound; rotate same label or force delete |

## 6.4 Dashboard

| Symptom | Fix |
|---------|-----|
| Looking for "Logs" | Tab is named **Runs** |
| Cron not editable in UI | Use `agentpaas cloud cron set/disable/enable` |
| Agents table empty after push | Hard refresh; confirm `cloud images` shows admitted |

## Commands that must work as printed

```bash
agentpaas version
agentpaas doctor
agentpaas cloud whoami
agentpaas cloud images
agentpaas cloud deployments
agentpaas cloud cron list
agentpaas cloud invoke <dep> --body '{"query":"San Francisco"}' --wait
```
