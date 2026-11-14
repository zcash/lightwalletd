A block with no transactions, or whose first transaction has no
transparent input, is now rejected by the parser instead of crashing the
process when its height is read. Consensus requires a coinbase with one
input, so only darkside mode or a misbehaving backend could supply such a
block (GHSA-56hr-g827-v3rj, GHSA-jcrq-vv43-968c).
