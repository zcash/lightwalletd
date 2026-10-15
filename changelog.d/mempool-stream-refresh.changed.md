`GetMempoolStream` no longer holds its shared lock while calling the
backend, so a slow backend no longer stalls every other stream client or
keeps them from noticing a cancel. A refresh whose client cancels no longer
hides the transactions it was fetching from every client until the next
block.
