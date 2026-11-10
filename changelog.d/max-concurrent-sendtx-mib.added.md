`--max-concurrent-sendtx-mib` bounds the transaction data, in MiB, that
`SendTransaction` works on at once; the default is 16. Each transaction is
copied several times on its way to the backend. Requests over the limit are
refused with `UNAVAILABLE`. Set it to 0 for no limit.
