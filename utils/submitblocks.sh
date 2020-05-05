#!/bin/bash
# Submits a list of blocks, one per line in the file, to darksidewalletd.
# Usage: ./submitblocks.sh <sapling activation> <file>
#   e.g. ./submitblocks.sh 1000 blocks.txt
#
set -e
test $# -ne 2 && { echo usage: $0 start-height blocks-file;exit 1;}

JSON="{\"saplingActivation\": $1, \"branchID\": \"2bb40e60\", \"chainName\": \"main\"}"
grpcurl -plaintext -d "$JSON" localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/SetMetaState

sed 's/^/{"block":"/;s/$/"}/' $2 |
grpcurl -plaintext -d @ localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/SetBlocks
