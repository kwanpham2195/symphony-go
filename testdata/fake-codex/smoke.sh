#!/bin/bash
# Fake codex app-server for smoke testing.
# Reads the protocol handshake, creates a file in the workspace, then completes.

# 1. initialize
read -r line
echo '{"id":1,"result":{"capabilities":{}}}'

# 2. initialized
read -r line

# 3. thread/start
read -r line
echo '{"id":2,"result":{"thread":{"id":"smoke-thread-001"}}}'

# 4. turn/start
read -r line
echo '{"id":3,"result":{"turn":{"id":"smoke-turn-001"}}}'

# Do the "work": create a file in the workspace
echo "hello from symphony-go" > hello.txt
echo "smoke test completed at $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> hello.txt

# Small delay to simulate work
sleep 0.2

# Complete the turn
echo '{"method":"turn/completed","params":{}}'
