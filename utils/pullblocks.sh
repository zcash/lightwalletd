#!/bin/bash
# Usage: ./pullblocks.sh 500000 500100 > blocks.txt
test $# -ne 2 && { echo usage: $0 start end;exit 1;}

let i=$1
while test $i -le $2
do
    zcash-cli getblock $i 0
    let i++
done
