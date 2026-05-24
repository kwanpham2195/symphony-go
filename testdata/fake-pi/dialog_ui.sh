#!/bin/bash
# Fake Pi RPC that emits a dialog extension_ui_request (select) mid-run.
# The client must respond with cancelled:true or the run hangs.
read -r line  # prompt command
echo '{"type":"response","command":"prompt","success":true,"id":"req-1"}'
echo '{"type":"agent_start"}'
echo '{"type":"turn_start"}'
# Dialog request: requires a response on stdin
echo '{"type":"extension_ui_request","id":"dlg-1","method":"select","title":"Pick one","options":["A","B"]}'
# Read the cancel response (client must send it)
read -r cancel_line
# Confirm request
echo '{"type":"extension_ui_request","id":"dlg-2","method":"confirm","title":"Sure?","message":"really?"}'
read -r confirm_line
# Complete the run
echo '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":15}},"toolResults":[]}'
echo '{"type":"agent_end","messages":[]}'
