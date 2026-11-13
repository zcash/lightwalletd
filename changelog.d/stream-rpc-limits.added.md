Lightwalletd bounds active streaming gRPCs across connections with
`--max-concurrent-stream-rpcs` (default 400, zero disables it). Both block-range
RPCs use their existing `--max-concurrent-blockrange` limit. Setting that flag
to zero disables their server-wide limit. The optional
`--max-concurrent-stream-rpcs-per-ip` cap defaults to zero (disabled). If
enabled, it uses the immediate TCP peer IP, so clients behind the same proxy
or shared address count toward one per-IP limit (#628).
