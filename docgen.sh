#!/bin/bash
#
# read argument files, construct simple html 
echo '<html>'
echo '<head>'
echo '<title>Lightwalletd reference API</title>'
echo '</head>'
echo '<body>'
echo '<h1>Lightwalletd API reference</h1>'
for f
do
    echo "<h2>$f</h2>"
    echo '<pre>'
    # list of reserved words https://developers.google.com/protocol-buffers/docs/proto3
    sed <$f '
        s/\/\/.*/<font color="grey">&<\/font>/
        s/\(^\|[^a-zA-Z_.]\)\(message\|service\|enum\)\($\|[^a-zA-Z_0-9]\)/\1<font color="red">\2<\/font>\3/
        s/\(^\|[^a-zA-Z_.]\)\(rpc\|reserved\|repeated\|enum|stream\)\($\|[^a-zA-Z_0-9]\)/\1<font color="green">\2\3<\/font>\3/
        s/\(^\|[^a-zA-Z_.]\)\(double\|float\|int32\|int64\|uint32\|uint64\|sint32\|sint64\|fixed32\|fixed64\|sfixed32\|sfixed64\|bool\|string\|bytes\)\($\|[^a-zA-Z_0-9]\)/\1<font color="blue">\2<\/font>\3/'
    echo '</pre>'
done
echo '</body>'
echo '</html>'
