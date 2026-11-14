Transparent-address validation compiles its regular expression once
instead of on every address, so a request with many addresses no longer
costs thousands of compilations (GHSA-x943-ghgq-v675).
