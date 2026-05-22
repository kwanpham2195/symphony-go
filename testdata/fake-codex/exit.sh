#!/bin/bash
# Fake codex that exits immediately after handshake.

read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line

read -r line
echo '{"id":2,"result":{"thread":{"id":"thread-exit"}}}'

read -r line
echo '{"id":3,"result":{"turn":{"id":"turn-exit"}}}'

exit 1
