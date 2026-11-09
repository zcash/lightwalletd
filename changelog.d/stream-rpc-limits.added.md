Lightwalletd bounds active streaming gRPCs across connections with
`--max-concurrent-stream-rpcs` (default 400) and per peer IP with
`--max-concurrent-stream-rpcs-per-ip` (default 100). Set either limit to zero
to disable it. The peer IP is the immediate TCP peer, so clients behind a
proxy or shared address count toward the same per-IP limit (#628).
