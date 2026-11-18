Revalidate a failed compact-block cache read before deferred corruption
recovery. A concurrent reorg can remove or replace the damaged block while
recovery waits for the write lock; that stale recovery now preserves the
repaired cache instead of clearing its valid blocks from disk.
