#!/bin/bash
# Fake codex app-server that completes a turn successfully.
# Reads JSON lines from stdin, responds with the expected protocol.

read -r line  # initialize request
echo '{"id":1,"result":{"capabilities":{}}}'

read -r line  # initialized notification

read -r line  # thread/start request
echo '{"id":2,"result":{"thread":{"id":"thread-abc"}}}'

read -r line  # turn/start request
echo '{"id":3,"result":{"turn":{"id":"turn-xyz"}}}'

# Simulate some work
sleep 0.1
echo '{"method":"turn/completed","params":{}}'
