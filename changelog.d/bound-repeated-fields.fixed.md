A request with a huge number of list elements is now refused before it is
decoded. An empty element takes 2 bytes on the wire but 16 to 24 bytes once
decoded, so a single 4 MiB request could cost almost 90 MiB before the
per-request limits on addresses or excluded txids could reject it, and a
few at once could exhaust the server's memory. The limits checked before
decoding are twice the existing ones, so a request just over a limit still
gets the same `InvalidArgument` as before.
