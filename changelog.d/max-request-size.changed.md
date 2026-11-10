The largest request lightwalletd accepts is now 2 MiB, down from gRPC's
default of 4 MiB. That still fits the largest transaction
`SendTransaction` accepts. This doesn't apply in darkside mode, and neither
does `--max-concurrent-recv`.
