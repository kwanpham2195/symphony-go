#!/bin/bash
# Fake codex app-server that writes the turn/start payload to TRACE_FILE.

read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line

read -r line
echo '{"id":2,"result":{"thread":{"id":"thread-trace"}}}'

read -r line
printf '%s\n' "$line" > "$TRACE_FILE"
echo '{"id":3,"result":{"turn":{"id":"turn-trace"}}}'

echo '{"method":"turn/completed","params":{}}'
