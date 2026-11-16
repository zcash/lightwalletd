`GetMempoolTx` no longer sorts the shared mempool list in place. Concurrent
callers each read that list without holding the lock, so the in-place sort let
one caller reorder it under another, which could send a transaction twice and
skip another. `MempoolFilter` now works on copies and leaves its arguments
untouched.
