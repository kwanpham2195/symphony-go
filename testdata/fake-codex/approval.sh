#!/bin/bash
# Fake codex app-server that requests command approval.

read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line

read -r line
echo '{"id":2,"result":{"thread":{"id":"thread-approve"}}}'

read -r line
echo '{"id":3,"result":{"turn":{"id":"turn-approve"}}}'

# Request approval
echo '{"id":"approval-1","method":"item/commandExecution/requestApproval","params":{"command":"ls -la"}}'

# Read approval response
read -r line

# Then complete
echo '{"method":"turn/completed","params":{}}'
