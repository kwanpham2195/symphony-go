#!/bin/bash
# Fake Pi RPC with compaction events mid-run.
read -r line  # prompt command
echo '{"type":"response","command":"prompt","success":true,"id":"req-1"}'
echo '{"type":"agent_start"}'
echo '{"type":"turn_start"}'
echo '{"type":"compaction_start","reason":"threshold"}'
sleep 0.1
echo '{"type":"compaction_end","reason":"threshold","result":{"summary":"compacted"},"aborted":false,"willRetry":false}'
echo '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done after compaction."}],"usage":{"input":200,"output":80,"cacheRead":0,"cacheWrite":0,"totalTokens":280}},"toolResults":[]}'
echo '{"type":"agent_end","messages":[]}'
