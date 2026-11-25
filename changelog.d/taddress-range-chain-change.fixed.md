`GetTaddressTransactions` now aborts before sending a transaction that is no
longer mined in the requested range. Clients can restart the request when
address-index results and later transaction lookups observe different chain
state.
