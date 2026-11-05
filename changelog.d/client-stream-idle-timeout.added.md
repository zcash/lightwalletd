`--client-stream-idle-timeout` (seconds, default 30) cancels a
client-streaming RPC whose client has sent nothing for that long, whether
before the first message or between messages. Without it, a client that
opens `GetTaddressBalanceStream` and sends no address parks a handler
goroutine indefinitely, so enough open streams exhaust memory. Set it to 0
to disable.
