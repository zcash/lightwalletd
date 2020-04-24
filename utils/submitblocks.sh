#!/bin/bash
# Submits a list of blocks, one per line in the file, to darksidewalletd.
# Usage: ./submitblocks.sh <start height> <sapling activation> <file>
#   e.g. ./submitblocks.sh 1000 1000 blocks.txt
set -e

JSON="{\"startHeight\": $1, \"saplingActivation\": $2, \"branchID\": \"2bb40e60\", \"chainName\": \"main\", \"blocks\": "
JSON="$JSON$(cat "$3" | sed 's/^/"/' | sed 's/$/"/' | sed '1s/^/[/;$!s/$/,/;$s/$/]/')"
JSON="$JSON}"
echo "$JSON"

grpcurl -plaintext -import-path ./walletrpc/ -proto service.proto -d "$JSON" localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/DarksideSetState
