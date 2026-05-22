#!/bin/bash
# Fake codex app-server that fails a turn.

read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line

read -r line
echo '{"id":2,"result":{"thread":{"id":"thread-fail"}}}'

read -r line
echo '{"id":3,"result":{"turn":{"id":"turn-fail"}}}'

echo '{"method":"turn/failed","params":{"reason":"model_error","message":"Something went wrong"}}'
