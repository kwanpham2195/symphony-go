#!/bin/bash
# Fake codex app-server that calls an unsupported tool.

read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line

read -r line
echo '{"id":2,"result":{"thread":{"id":"thread-tool"}}}'

read -r line
echo '{"id":3,"result":{"turn":{"id":"turn-tool"}}}'

# Call unsupported tool
echo '{"id":"tool-1","method":"item/tool/call","params":{"tool":"unknown_tool","arguments":{"query":"test"}}}'

# Read tool response
read -r line

# Then complete
echo '{"method":"turn/completed","params":{}}'
