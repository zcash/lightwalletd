#!/bin/bash
# Usage: ./pullblocks.sh 500000 500100 > blocks.txt

for i in $(seq $1 $2); do
	zcash-cli getblock $i 0
done
