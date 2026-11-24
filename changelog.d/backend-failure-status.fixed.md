Transparent balance and UTXO RPCs now report unclassified backend failures as
`Unknown` instead of returning a successful zero balance or empty UTXO result.
This applies to unary and streaming variants; the existing `InvalidArgument`
and `NotFound` mappings are unchanged.

`GetTransaction` now reserves `NotFound` for the backend's structured
missing-transaction response. Other backend failures return `Unknown`;
canceled and timed-out backend requests retain their context status.
