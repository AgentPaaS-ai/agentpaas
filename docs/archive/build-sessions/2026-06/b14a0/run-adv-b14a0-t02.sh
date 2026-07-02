PROMPT="$(cat /tmp/b14a0-t02-adversary-prompt.md)"
cd /Users/pms88/projects/agentpaas
hermes -p agentpaas-adversary chat -q "$PROMPT" -Q --toolsets terminal,file,search 2>&1 | tee /tmp/b14a0-t02-adversary.log
echo "===ADV_EXIT=$?==="
