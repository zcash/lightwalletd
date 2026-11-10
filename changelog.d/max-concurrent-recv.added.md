`--max-concurrent-recv` bounds how many requests the server receives at
once across every connection; the default is 32. gRPC reads a whole request
into memory before any handler runs, so many large requests arriving at
once could exhaust memory. A request over the limit is refused with
`UNAVAILABLE` before its body is read, and one whose body hasn't arrived
within 20 seconds is canceled. Client-streaming calls count against a
separate pool of the same size for as long as they're open. Set it to 0 for
no limit.

When a pool is full, a new request can take the oldest slot of the
connection holding the most, so one connection can't lock out the rest.
Clients spread over many connections still can, so a per-IP connection
limit at a proxy or firewall is a good idea.
