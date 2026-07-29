#!/bin/bash
# Fetches raw (hex-encoded) blocks from a Zcash node using JSON-RPC,
# one block per line, for use with submitblocks.sh (darksidewalletd).
# Usage: ./pullblocks.sh 500000 500100 > blocks.txt
# Set RPCHOST and RPCPORT to override the default node RPC endpoint.
test $# -ne 2 && { echo usage: $0 start end;exit 1;}

RPCHOST=${RPCHOST:-127.0.0.1}
RPCPORT=${RPCPORT:-8232}

let i=$1
while test $i -le $2
do
    curl -s -X POST -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":"pullblocks","method":"getblock","params":["'$i'",0]}' \
        "http://$RPCHOST:$RPCPORT/" | jq -r .result
    let i++
done
