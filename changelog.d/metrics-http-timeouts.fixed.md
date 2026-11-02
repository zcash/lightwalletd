The Prometheus metrics HTTP listener (`--http-bind-addr`) now has request
timeouts: 10 seconds for a request's headers to arrive, 30 seconds for the
whole request and for the response, and 60 seconds idle. Before, a peer
that sent a partial request held its connection, and a file descriptor,
for as long as it liked, and enough of them starved the gRPC listener in
the same process (GHSA-w24w-x7vw-f2w8). A failure to start the listener is
now logged instead of ignored.
