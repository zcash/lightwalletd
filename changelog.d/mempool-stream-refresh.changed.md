`GetMempoolStream` no longer holds its shared lock while calling the
backend, so a slow backend no longer stalls every other stream client or
keeps them from noticing a cancel. A client that cancels no longer stops
the refresh it started, and a transaction that fails to parse is skipped
rather than failing every refresh until the next block.
