#!/bin/bash
# Fake Pi RPC with extension_ui_request events (fire-and-forget).
read -r line  # prompt command
echo '{"type":"extension_ui_request","id":"ui-1","method":"setStatus","statusKey":"ext"}'
echo '{"type":"extension_ui_request","id":"ui-2","method":"setWidget","widgetKey":"ext"}'
echo '{"type":"response","command":"prompt","success":true,"id":"req-1"}'
echo '{"type":"agent_start"}'
echo '{"type":"turn_start"}'
echo '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":15}},"toolResults":[]}'
echo '{"type":"agent_end","messages":[]}'
