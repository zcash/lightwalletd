# Intro to darksidewalletd

Darksidewalletd is a feature included in lightwalletd, enabled by the
`--darkside-very-insecure` flag, which can serve arbitrary blocks to a Zcash
light client wallet. This is useful for security and reorg testing. It includes
a minimally-functional mock zcashd which comes with a gRPC API for controlling
which blocks it will serve.

This means that you can use darksidewalletd to control the blocks and
transactions that are exposed to any light wallets that connect, to see how
they behave under different circumstances. Multiple wallets can connect to
the same darksidewalletd at the same time. Darksidewalletd should only be
used for testing, and therefore is hard-coded to shut down after 30 minutes
of operation to prevent accidental deployment as a server.

## Security warning

Leaving darksidewalletd running puts your machine at greater risk because (a)
it may be possible to use file: paths with `StageBlocks` to read arbitrary
files on your system, and (b) also using `StageBlocks`, someone can force
your system to make a web request to an arbitrary URL (which could have your
system download questionable material, perform attacks on other systems,
etc.). The maximum 30-minute run time limit built into darksidewalletd
mitigates these risks, but users should still be cautious.

## Dependencies 

Lightwalletd and most dependencies of lightwalletd, including Go version 1.11 or
later, but not zcashd. Since Darksidewalletd mocks zcashd, it can run standalone
and does not use zcashd to get blocks or send and receive transactions.

For the tutorial the `grpcurl` tool is needed to call the `darksidewalletd`
gRPC API.

## Overview

### How Darksidewalletd Works

Lightwalletd and the wallets themselves don’t actually perform any validation
of the blocks (beyond checking the blocks’ prevhashes, which is used to
detect reorgs). That means the blocks we give darksidewalletd don’t need to
be fully valid, see table:

Block component|Must be valid|Must be partially valid|Not checked for validity 
:-----|:-----|:-----|:-----
nVersion|x| | 
hashPrevBlock|x| | 
hashMerkleRoot| | |x 
hashFinalSaplingRoot| | |x 
nTime| | |x 
nBits| | |x 
nNonce| | |x 
Equihash solution| | |x 
Transaction Data*| |x|  

\*Transactions in blocks must conform to the transaction format, but not need
valid zero-knowledge proofs etc.

For more information about block headers, see the Zcash protocol specification.

Lightwalletd provides us with a gRPC API for generating these
minimally-acceptable fake blocks. The API allows us to "stage" blocks and
transactions and later "apply" the staged objects so that they become visible
to lightwalletd and the wallets. How this is done is illustrated in the
tutorial below, but first we must start darksidewalletd.

### Running darksidewalletd

To start darksidewalletd, you run lightwalletd with the
`--darkside-very-insecure` flag:

```
./lightwalletd --darkside-very-insecure --no-tls-very-insecure --data-dir . --log-file /dev/stdout
```

To prevent accidental deployment in production, it will automatically shut off
after 30 minutes.

Now that `darksidewalletd` is running, you can control it by calling various
gRPCs to reset its state, stage blocks, stage transactions, and apply the
staged objects so that they become visible to the wallet. Examples of using
these gRPCs are given in the following tutorial.

## Tutorial

This tutorial is intended to illustrate basic control of `darksidewalletd`
using the `grpcurl` tool. You can use any gRPC library of your choice in
order to implement similar tests in your apps' test suite.

### Simulating a reorg that moves a transaction

In this example, we will simulate a reorg that moves a transaction from one
block height to another. This happens in two parts, first we create and apply
the "before reorg" state. Then we create the "after reorg" stage and apply
it, which makes the reorg happen.

Here's a quick-start guide to simulating a reorg:
```
grpcurl -plaintext -d '{"saplingActivation": 663150,"branchID": "bad", "chainName":"x"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/Reset
grpcurl -plaintext -d '{"url": "https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/basic-reorg/663150.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocks
grpcurl -plaintext -d '{"height":663151,"count":10}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksCreate
grpcurl -plaintext -d '{"height":663160}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
grpcurl -plaintext -d '{"height":663155,"count":10,"nonce":44}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksCreate
grpcurl -plaintext -d '{"height":663164}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
```

#### Creating the Before-Reorg State

If you haven't already started darksidewalletd, please start it:

```
./lightwalletd --darkside-very-insecure --no-tls-very-insecure --data-dir . --log-file /dev/stdout
```

First, we need to reset darksidewalletd, specifying the sapling activation
height, branch ID, and chain name that will be told to wallets when they ask:

```
grpcurl -plaintext -d '{"saplingActivation": 663150,"branchID": "bad", "chainName":"x"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/Reset
```

Next, we will stage the real mainnet block 663150. In ECC's example wallets, this block is used as a checkpoint so we need to use the real block to pass that check.

```
grpcurl -plaintext -d '{"url": "https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/basic-reorg/663150.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocks
```

This has put block 663150 into darksidewalletd's staging area. The block has
not yet been exposed to the internal block-processing mechanism in
lightwalletd, and thus any wallets connected will have no idea it exists yet.

Next, we will use the `StageBlocksCreate` gRPC to generate 100 fake blocks on top of 663150 in darksidewalletd's staging area:

```
grpcurl -plaintext -d '{"height":663151,"count":100}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksCreate
```

Still, everything is in darksidewalletd's staging area, nothing has been
shown to any connected wallets yet. The staging area now contains the real
mainnet block 663150 and 100 fake blocks from 663151 to 663250.

Next we'll stage a transaction to go into block 663190. 663190 is within the
range of blocks we've staged; when we "apply" the staging area later on
darksidewalletd will merge this transaction into the fake 663190 block.

```
grpcurl -plaintext -d '{"height":663190,"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/transactions/recv/0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageTransactions
```

We have now finished filling darksidewalletd's staging area with the "before
reorg" state blocks. In darksidewalletd's staging area, we have blocks from
663150 to 663250, with a transaction staged to go in block 663190. All that's
left to do is "apply" the staging area, which will reveal the blocks to
lightwalletd's internal block processor and then on to any wallets that are
connected. We will apply the staged blocks up to height 663210 (any higher
staged blocks will remain in the staging area):

```
grpcurl -plaintext -d '{"height":663210}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
```

Note that we could have done this in the opposite order, it would have been
okay to stage the transaction first, and then stage the blocks later. All
that matters is that the transactions we stage get staged into block heights
that will have blocks staged for them before we "apply".

Now we can check that the transaction is in block 663190:

```
$ grpcurl -plaintext -d '{"height":663190}' localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/GetBlock
{
  "height": "663190",
  "hash": "Ax/AHLeTfnDuXWX3ZiYo+nWvh24lyMjvR0e2CAfqEok=",
  "prevHash": "m5/epQ9d3wl4Z8bctOB/ZCuSl8Uko4DeIpKtKZayK4U=",
  "time": 1,
  "vtx": [
    {
      "index": "1",
      "hash": "H0nPz83r1cuQhdn/LvvNqHEh3aE/LHkRE/zy55uoIQg=",
      "spends": [
        {
          "nf": "xrZLCu+Kbv6PXo8cqM+f25Hp55L2cm95bM68JwUnDHg="
        }
      ],
      "outputs": [
        {
          "cmu": "pe/G9q13FyE6vAhrTPzIGpU5Dht5DvJTuc9zmTEx0gU=",
          "epk": "qw5MPsRoe8aOnvZ/VB3r1Ja/WkHb52TVU1vyHjGEOqc=",
          "ciphertext": "R2uN3CHagj7Oo+6O9VeBrE6x4dQ07Jl18rVM27vGhl1Io75lFYCHA1SrV72Zu+bgwMilTA=="
        },
        {
          "cmu": "3rQ9DMmk7RaWGf9q0uOYQ7FieHL/TE8Z+QCcS/IJfkA=",
          "epk": "U1NCOlTzIF1qlprAjuGUUj591GpO5Vs5WTsmCW35Pio=",
          "ciphertext": "2MbBHjPbkDT/GVsXgDHhihFQizxvizHINXKVbXKnv3Ih1P4c1f3By+TLH2g1yAG3lSARuQ=="
        }
      ]
    }
  ]
}
$ 
```

#### Creating the After-Reorg State

Now, we can stage that same transaction into a different height, and force a
reorg.

First, stage 100 fake blocks starting at height 663180. This stages empty
blocks for heights 663180 through 663279. These are the blocks that will
change after the reorg.

```
grpcurl -plaintext -d '{"height":663180,"count":100}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksCreate
```

Now, stage that same transaction as before, but this time to height 663195
(previously we had put it in 663190):

```
grpcurl -plaintext -d '{"height":663195,"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/transactions/recv/0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageTransactions
```

Finally, we can apply the staged blocks and transactions to trigger a reorg:

```
grpcurl -plaintext -d '{"height":663210}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
```

This will simulate a reorg back to 663180 (new versions of 663180 and
beyond, same 663179), and the transaction will now be included in 663195
and will _not_ be in 663190.

After a moment you should see some "reorg" messages in the lightwalletd log
output to indicate that lightwalletd's internal block processor detected and
handled the reorg. If a wallet were connected to the lightwalletd instance,
it should also detect a reorg too.

Now we can check that the transaction is no longer in 663190:

```
$ grpcurl -plaintext -d '{"height":663190}' localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/GetBlock
{
  "height": "663190",
  "hash": "btosPfiJBX9m3nNSCP+vjAxWpEDS7Kfut9H7FY+mSYo=",
  "prevHash": "m5/epQ9d3wl4Z8bctOB/ZCuSl8Uko4DeIpKtKZayK4U=",
  "time": 1
}
$
```

Instead, it has "moved" to 663195:

```
$ grpcurl -plaintext -d '{"height":663195}' localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/GetBlock
{
  "height": "663195",
  "hash": "CmcEQ/NZ9nSk+VdNfCEHvKu9MTNeWKoF1dZ7cWUTnCc=",
  "prevHash": "04i1neRIgx7vgtDkrydYJu3KWjbY5g7QvUygNBfu6ug=",
  "time": 1,
  "vtx": [
    {
      "index": "1",
      "hash": "H0nPz83r1cuQhdn/LvvNqHEh3aE/LHkRE/zy55uoIQg=",
      "spends": [
        {
          "nf": "xrZLCu+Kbv6PXo8cqM+f25Hp55L2cm95bM68JwUnDHg="
        }
      ],
      "outputs": [
        {
          "cmu": "pe/G9q13FyE6vAhrTPzIGpU5Dht5DvJTuc9zmTEx0gU=",
          "epk": "qw5MPsRoe8aOnvZ/VB3r1Ja/WkHb52TVU1vyHjGEOqc=",
          "ciphertext": "R2uN3CHagj7Oo+6O9VeBrE6x4dQ07Jl18rVM27vGhl1Io75lFYCHA1SrV72Zu+bgwMilTA=="
        },
        {
          "cmu": "3rQ9DMmk7RaWGf9q0uOYQ7FieHL/TE8Z+QCcS/IJfkA=",
          "epk": "U1NCOlTzIF1qlprAjuGUUj591GpO5Vs5WTsmCW35Pio=",
          "ciphertext": "2MbBHjPbkDT/GVsXgDHhihFQizxvizHINXKVbXKnv3Ih1P4c1f3By+TLH2g1yAG3lSARuQ=="
        }
      ]
    }
  ]
}
$
```

Just to illustrate a little more about how `ApplyStaged` works, we can check
that the current height is 663210 just like we specified in our last call to
`ApplyStaged`:

```
$ grpcurl -plaintext -d '' localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/GetLatestBlock
{
  "height": "663210"
}
```

Then apply 10 more of the blocks that are still in the staging area:

```
grpcurl -plaintext -d '{"height":663220}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
```

And confirm that the current height has increased:

```
$ grpcurl -plaintext -d '' localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/GetLatestBlock
{
  "height": "663220"
}
```

That concludes the tutorial. You should now know how to stage blocks from a
URL using `StageBlocks`, stage synthetic empty blocks using
`StageBlocksCreate`, stage transactions from a URL to go into particular
blocks using `StageTransactions`, and then make the staged blocks and
transactions live using `ApplyStaged`.

On top of what we covered in the tutorial, you can also...

- Stage blocks and transactions directly (without them having to be
accessible at a URL) using `StageBlocksStream` and `StageTransactionsStream`.
- Get all of the transactions sent by connected wallets using
`GetIncomingTransactions` (and clear the buffer that holds them using
`ClearIncomingTransactions`).

See [darkside.proto](/walletrpc/darkside.proto) for a complete definition of
all the gRPCs that darksidewalletd supports.

## Generating Fake Block Sets

There’s a tool to help with generating these fake just-barely-valid-enough
blocks, it’s called genblocks. To use it you create a directory of text files,
one file per block, and each line in the file is a hex-encoded transaction that
should go into that block:

```
mkdir blocksA
touch blocksA/{1000,1001,1002,1003,1004,1005}.txt
echo “some hex-encoded transaction you want to put in block 1003” > blocksA/1003.txt
```

This will output the blocks, one hex-encoded block per line. This is the
format that will be accepted by `StageBlocks`.

Tip: Because nothing is checking the full validity of transactions, you can get
any hex-encoded transaction you want from a block explorer and put those in the
block files. The sochain block explorer makes it easy to obtain the raw
transaction hex, by viewing the transaction (example), clicking “Raw Data”, then
copying the “tx_hex” field.

### Simulating the mempool

The `GetMempoolTx` gRPC will return staged transactions that are either within
staged blocks or that have been staged separately. Here is an example:
```
grpcurl -plaintext -d '{"saplingActivation": 663150,"branchID": "bad", "chainName":"x"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/Reset
grpcurl -plaintext -d '{"url": "https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/tx-incoming/blocks.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocks
grpcurl -plaintext -d '{"txid":["qg=="]}' localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/GetMempoolTx
```

## Use cases

Check out some of the potential security test cases here: [wallet <->
lightwalletd integration
tests](https://github.com/zcash/lightwalletd/blob/master/docs/integration-tests.md)
