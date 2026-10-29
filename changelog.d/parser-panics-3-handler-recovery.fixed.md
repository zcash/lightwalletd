A panic in a gRPC handler now fails that one request with `Internal` and
logs a stack trace, instead of ending the process.
