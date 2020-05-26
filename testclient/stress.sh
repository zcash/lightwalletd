#!/bin/bash
#
# Create a CSV file with various performance measurements
#
set -e
test $# -eq 0 && { echo "usage: $0 iterations op(getlighdinfo|getblock|getblockrange)";exit 1;}
iterations=$1
op=$2
export p=`pidof server`
test -z $p && { echo 'is the server running?';exit 1;}
set -- $p
test $# -ne 1 && { echo 'server pid is not unique';exit 1;}
echo "concurrency,iterations per thread,utime before (ticks),stime before (ticks),memory before (pages),time (sec),utime after (ticks),stime after (ticks),memory after (pages)"
for i in 1 200 400 600 800 1000
do
    csv="$i,$iterations"
    csv="$csv,`cat /proc/$p/stat|field 14`" # utime in 10ms ticks
    csv="$csv,`cat /proc/$p/stat|field 15`" # stime in 10ms ticks
    csv="$csv,`cat /proc/$p/statm|field 2`" # resident size in pages (8k)
    csv="$csv,`/usr/bin/time -f '%e' testclient/main -concurrency $i -iterations $iterations -op $op 2>&1`"
    csv="$csv,`cat /proc/$p/stat|field 14`" # utime in 10ms ticks
    csv="$csv,`cat /proc/$p/stat|field 15`" # stime in 10ms ticks
    csv="$csv,`cat /proc/$p/statm|field 2`"
    echo $csv
done
