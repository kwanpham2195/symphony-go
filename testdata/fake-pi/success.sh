#!/bin/bash
# Fake Pi RPC that completes a prompt successfully.
read -r line  # prompt command
echo '{"type":"response","command":"prompt","success":true,"id":"req-1"}'
echo '{"type":"agent_start"}'
echo '{"type":"turn_start"}'
sleep 0.1
echo '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"usage":{"input":100,"output":50,"cacheRead":40,"cacheWrite":5,"totalTokens":195}},"toolResults":[]}'
echo '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"Done."}]}]}'
