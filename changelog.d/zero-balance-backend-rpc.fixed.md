- The `GetTaddressBalance` and `GetAddressUtxos` gRPC methods no longer
  report a successful zero balance or empty UTXO list when the backend RPC
  fails with an error other than "Invalid address" or "No information
  available" (for example, while zcashd restarts): the fallback gRPC status
  code was an uninitialized zero value, `codes.OK`, and `status.Errorf`
  returns a nil error for `codes.OK`.
