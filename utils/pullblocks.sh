#!/bin/bash
# Usage: ./pullblocks.sh 500000 500100 > blocks.txt
test $# -ne 2 && { echo usage: $0 start end;exit 1;}

for i in $(seq $1 $2); do
	zcash-cli getblock $i 0
done
