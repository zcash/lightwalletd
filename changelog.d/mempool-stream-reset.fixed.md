- A `GetMempoolStream` call that was waiting on a slow client could panic if
  the mempool list was reset twice meanwhile and the tip hash was the same
  again. In 0.6.1 that ended the process; in 0.6.2 it left a lock held, so
  every later `GetMempoolStream` call blocked until a restart
  (GHSA-fhv5-hm8v-q7fw).
