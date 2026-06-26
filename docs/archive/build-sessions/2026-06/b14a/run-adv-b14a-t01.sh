#!/bin/bash
PROMPT="$(cat /tmp/b14a-t01-adversary-prompt.md)"
cd /Users/example/projects/agentpaas
git checkout feat/b14a-t01 2>/dev/null
hermes -p agentpaas-adversary chat -q "$PROMPT" -Q --toolsets terminal,file,search
