Updated `golang.org/x/net` to v0.60.0, which fixes five HTTP/2 server
vulnerabilities reachable through gRPC (GO-2026-6603, GO-2026-6610,
GO-2026-6611, GO-2026-6612, GO-2026-6617). Two of them let a client crash
the process or exhaust its memory.
