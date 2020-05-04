# Wallet ⟷ Lightwalletd Integration Tests

## High-priority tests
Funds are at risk if these tests fail. 

**Reorged-Away Transaction** 
A transparent/shielded transaction is sent to the wallet in block N containing value v. There's a reorg to height N-k for some k >= 1, and after the reorg the original transaction is no longer there but there is a new transaction with a different value u. Before the reorg, the wallet should detect the transaction and show unconfirmed balance v. After the reorg, the wallet should show unconfirmed balance u. Some number of blocks later, the balance is marked as confirmed.

Consequences if this test fails: An attacker could take advantage of regular/accidental reorgs to confuse the wallet about its balance.

**Dropped from Mempool**
Similar to the reorged-away transaction test, except the transaction only enters the mempool and is never mined.

Consequences: An attacker could confuse wallets about their balance by arranging for a transaction to enter the mempool but not be mined.

**Transparent TXID Malleated**
The wallet sends a transparent transaction. Its transaction ID is malleated to change its transaction ID, and then mined. After sending the transaction, the wallet’s balance should be reduced by the value of the transaction. 100 blocks after the transparent transaction was mined, the wallet’s balance should still be reduced by that amount.

Consequences if this test fails: An attacker could malleate one of the wallet’s transparent transactions, and if it times out thinking it was never mined, the wallet would think it has balance when it doesn’t.

**Transaction Never Mined**
The wallet broadcasts a transparent/shielded transaction optionally with an expiry height. For 100 blocks (or at least until the expiry height), the transaction is never mined. After sending the transaction, the wallet’s balance should be reduced by the value of the transaction, and it should stay reduced by that amount until the expiry height (if any).

Consequences if this test fails: If the wallet concludes the transaction will never be mined before the expiry height, then an attacker can delay mining the transaction to cause the wallet to think it has more funds than it does.

**Transaction Created By Other Wallet**
A seed is imported into three wallets with transparent/shielded funds. Wallet A sends a payment to some address. At the same time, Wallet B sends a payment of a different amount using some of the same UTXOs or notes. Wallet C does not send any payments. Wallet B’s transaction gets mined instead of Wallet A’s. The balances of all three wallets are decreased by the value of Wallet B’s transaction.

Consequences if this test fails: A user importing their seed into multiple wallets and making simultaneous transactions could lead them to be confused about their balance.

**Anchor Invalidation**
A wallet broadcasts a sapling transaction using a recent anchor. A reorg occurs which invalidates that anchor, i.e. some of the previous shielded transactions changed. (Depending on how we want to handle this) the wallet either detects this and re-broadcasts the transaction or marks the transaction as failed and the funds become spendable again.

Consequences if this test fails: Wallets might get confused about their balance if this ever occurs.

**Secret Transactions**
Lightwalletd has some shielded/transparent funds. It creates a real transaction sending these funds to the wallet, such that if the transaction were broadcast on the Zcash network, the wallet really would get the funds. However, instead of broadcasting the transaction, the lightwalletd operator includes the transaction in a compact block, but does not broadcast the transaction to the actual network. The wallet should detect that the transaction has not really been mined by miners on the Zcash network, and not show an increased balance.

(Currently, this test will fail, since the wallet is not checking the PoW or block headers at all. Worse, even with PoW/header checks, lightwalletd could mine down the difficulty to have their wallets follow an invalid chain. To comabt this wallets would need to reach out to multiple independent lightwalletds to verify it has the highest PoW chain. Or Larry’s idea, to warn when the PoW is below a certain threshold (chosen to be above what most attackers could do but low enough we’d legitimately want to warn users if it drops that low), I like a lot better.)

Consequences if this test fails: lightwalletd can make it appear as though a wallet received funds when it didn’t.

## Medium-priority tests
Funds aren’t at risk if these fail but there’s a severe problem. 

**Normal Payments**
Wallet A sends a shielded/transparent transaction to Wallet B. Wallet B receives the transaction and sends half back to wallet A. Wallet A receives the transaction, and B receives change. All of the balances end up as expected.

Consequences if this test fails: Normal functionality of the wallet is broken.

**Mempool DoS**
The transactions in the mempool constantly churn. The wallet should limit its bandwidth used to fetch new transactions in the mempool, rather than using an unlimited amount.

Consequences if this test fails: It’s possible to run up the bandwidth bills of wallet users.


## Low-priority tests
 These won’t occur unless lightwalletd is evil. 

**High Block Number**
Lightwalletd announces that the latest block is some very large number, much larger than the actual block height. The wallet syncs up to that point (with lightwalletd providing fake blocks all the way). Lightwalletd then stops lying about the block height and blocks. This should trigger the wallet’s reorg limit and the wallet should be unusable.

**Repeated Note**
A shielded transaction is sent to the wallet. Lightwalletd simply repeats the transaction in a compact block sent to the wallet. The wallet should not think it has twice as much money. From this point, no shielded transactions the wallet sends can be mined, since they will use invalid anchors.

**Invalid Note**
Same as repeated note above, but random data. The results should be exactly the same.

**Omitted Note**
A shielded transaction is sent to the wallet. Lightwalletd simply does not send the transaction to the wallet (omits it from the compact block).  From this point, no shielded transactions the wallet sends can be mined, since they will use invalid anchors.

