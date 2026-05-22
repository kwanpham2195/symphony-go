#!/bin/bash
# Fake codex app-server that requests user input.

read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line

read -r line
echo '{"id":2,"result":{"thread":{"id":"thread-input"}}}'

read -r line
echo '{"id":3,"result":{"turn":{"id":"turn-input"}}}'

echo '{"id":"input-1","method":"item/tool/requestUserInput","params":{"prompt":"Enter your name"}}'
