#!/bin/bash
# Fake Pi RPC that rejects the prompt.
read -r line  # prompt command
echo '{"type":"response","command":"prompt","success":false,"error":"Model not found","id":"req-1"}'
