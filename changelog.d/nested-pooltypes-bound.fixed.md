- The limit on repeated request fields checked before decoding now also
  covers `poolTypes` in the `range` of a `TransparentAddressBlockFilter`
  (`GetTaddressTxids`, `GetTaddressTransactions`), which the 0.6.2 fix
  missed (GHSA-jjjw-r66r-mjgf).
