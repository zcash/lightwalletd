#!/bin/bash
# Submits a list of blocks, one per line in the file, to darksidewalletd.
# Usage: ./submitblocks.sh <sapling activation> <file>
#   e.g. ./submitblocks.sh 1000 blocks.txt
#
set -e
test $# -ne 2 && { echo usage: $0 sapling-height blocks-file;exit 1;}

# must do a Reset first
grpcurl -plaintext -d '{"saplingActivation":'$1',"branchID":"2bb40e60","chainName":"main"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/Reset

# send the blocks and make them active
sed 's/^/{"block":"/;s/$/"}/' $2 |
grpcurl -plaintext -d @ localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksStream
let latest=$1+$(cat $2|wc -l)-1
grpcurl -plaintext -d '{"height":'$latest'}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
