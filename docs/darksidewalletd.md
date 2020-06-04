# Intro to darksidewalletd

Darksidewalletd is a feature included in lightwalletd, enabled by the
`--darkside-very-insecure` flag, which can serve arbitrary blocks to a zcash
light client wallet. This is useful for security and reorg testing. It includes
a minimally-functional mock zcashd which comes with a gRPC API for controlling
which blocks it will serve.

This means that you can use darksidewalletd to alter the sequence of blocks, the
blocks and information inside the blocks, and much more--then serve it to
a light client wallet to see how it behaves. Multiple wallets can connect to the
same darksidewalletd at the same time. Darksidewalletd should only be used for
testing, and therefore is hard-coded to shut down after 30 minutes of operation
to prevent accidental deployment as a server.

## Security warning

Leaving darksidewalletd running puts your machine at greater risk because (a) it
may be possible to use file: paths with `DarksideSetBlocksURL` to read arbitrary
files on your system, and (b) also using `DarksideSetBlocksURL`, someone can
force your system to make a web request to an arbitrary URL (which could have
your system download questionable material, perform attacks on other systems,
etc.). The maximum 30-minute run time limit built into darksidewalletd mitigates
these risks, but users should still be cautious.

## Dependencies 

Lightwalletd and most dependencies of lightwalletd, including Go version 1.11 or
later, but not zcashd. Since Darksidewalletd mocks zcashd, it can run standalone
and does use zcashd to get blocks or send and receive transactions.

For the tutorial (and further testing) the tool grpcurl will be used to call the
API and set blocks.

## Overview
### Running darksidewalletd

To start darksidewalletd, you run lightwalletd with a flag:

`./lightwalletd --darkside-very-insecure`

To prevent accidental deployment in production, it will automatically shut off
after 30 minutes.

### Default set of blocks

There’s a file in the repo called ./testdata/darkside/init-blocks. This
contains the blocks darksidewalletd loads by default. The format of the file is
one hex-encoded block per line.

### Generating fake blocks with genblocks

Lightwalletd and the wallets themselves don’t actually perform any validation of
the blocks (beyond checking the blocks’ prevhashes, which is used to detect
reorgs). For information on block headers, see the zcash protocol specification.
That means the blocks we give darksidewalletd don’t need to be fully valid, see
table:


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

There’s a tool to help with generating these fake just-barely-valid-enough
blocks, it’s called genblocks. To use it you create a directory of text files,
one file per block, and each line in the file is a hex-encoded transaction that
should go into that block:

```
mkdir blocksA
touch blocksA/{1000,1001,1002,1003,1004,1005}.txt
echo “some hex-encoded transaction you want to put in block 1003” > blocksA/1003.txt
```

This will output the blocks, one hex-encoded block per line (the same format as
./testdata/darkside/init-blocks). 

Tip: Because nothing is checking the full validity of transactions, you can get
any hex-encoded transaction you want from a block explorer and put those in the
block files. The sochain block explorer makes it easy to obtain the raw
transaction hex, by viewing the transaction (example), clicking “Raw Data”, then
copying the “tx_hex” field.

### Using DarksideSetState to submit a new set of blocks

As mentioned, the darksidewalletd PR adds an RPC, it’s called DarksideSetState,
which lets you control the blocks the mock zcashd is serving. Well, it would let
you if you could speak gRPC. If you want to do it manually (not part of some
test code) you can use a tool called grpcurl to call the API and set the blocks.
Once you have that installed, there’s a script in utils/submitblocks.sh to
submit the blocks, which internally uses grpcurl, e.g.:

```
./genblocks --blocks-dir blocksA > blocksA.txt
./utils/submitblocks.sh 1000 blocksA.txt
```

In the submitblocks.sh command, the “1000” sets the value that lightwalletd will
report the sapling activation height to be.

Tip: You may submit blocks incrementally, that is, submit 1000-1005 followed
by 1006-1008, the result is 1000-1008. You can't create a gap in the range (say,
1000-1005 then 1007-1009).

If you submit overlapping ranges, the expected things happen. For example, first
submit 1000-1005, then 1003-1007, the result is 1000-1007 (the original 1000-1002
followed by the new 1003-1007). This is how you can create a reorg starting at 1003.
You can get the same effect slightly less efficiently by submitting 1000-1007 (that
is, resubmitting the original 1000-1002 followed by the new 1003-1007).

If you first submit 1000-1005, then 1001-1002, the result will be 1000-1002
(1003-1005 are dropped; it's not possible to "insert" blocks into a range).
Likewise, first submit 1005-1008, then 1000-1006, the result is only 1000-1006. An
easy way to state it is that all earlier blocks beyond the end of the extent of
the range being submitted now are dropped. But blocks before the start of the range
being submitted now are preserved if doing so doesn't create a gap.

## Tutorial
### Triggering a Reorg

To begin following these instructions, build lightwalletd but do not start
darksidewalletd yet. If you started it during the overview above, kill the
server before starting the tutorial. 

We’ll use genblocks to generate the hex-encoded blocks, then
./utils/submitblocks.sh to get them into darksidewalletd. We’ll call the blocks
before the reorg “blocksA” and the blocks after the reorg “blocksB”:

```
mkdir blocksA
touch blocksA/{1000,1001,1002,1003,1004,1005}.txt
mkdir blocksB
touch blocksB/{1000,1001,1002,1003,1004,1005,1006}.txt
echo "0400008085202f8901950521a79e89ed418a4b506f42e9829739b1ca516d4c590bddb4465b4b347bb2000000006a4730440220142920f2a9240c5c64406668c9a16d223bd01db33a773beada7f9c9b930cf02b0220171cbee9232f9c5684eb918db70918e701b86813732871e1bec6fbfb38194f53012102975c020dd223263d2a9bfff2fa6004df4c07db9f01c531967546ef941e2fcfbffeffffff026daf9b00000000001976a91461af073e7679f06677c83aa48f205e4b98feb8d188ac61760356100000001976a91406f6b9a7e1525ee12fd77af9b94a54179785011b88ac4c880b007f880b000000000000000000000000" > blocksB/1004.txt
```

Use genblocks to put together the fake blocks:

```
./genblocks --blocks-dir blocksA > testdata/default-darkside-blocks
./genblocks --blocks-dir blocksB > testdata/darkside-blocks-reorg
```

(note: this is overwrites the file darksidewalletd loads by default, testdata/default-darkside-blocks)

Now you can start darksidewalletd and it’ll load the blocksA blocks:

`./lightwalletd --darkside-very-insecure`

That will have loaded and be serving the blocksA blocks. We can push up the
blocksB blocks using ./utils/submitblocks.sh:

`./utils/submitblocks.sh 1000 testdata/darkside-blocks-reorg`

We should now see a reorg in server.log:

```
{"app":"frontend-grpc","duration":442279,"error":null,"level":"info","method":"/cash.z.wallet.sdk.rpc.CompactTxStreamer/DarksideSetState","msg":"method called","peer_addr":{"IP":"127.0.0.1","Port":47636,"Zone":""},"time":"2020-03-23T13:59:41-06:00"}
{"app":"frontend-grpc","hash":"a244942179988ea6e56a3a55509fcf22673df26200c67bebd93504385a1a7c4f","height":1004,"level":"warning","msg":"REORG","phash":"06e7c72646e3d51417de25bd83896c682b72bdf5be680908d621cba86d222798","reorg":1,"time":"2020-03-23T13:59:44-06:00"}
```

### Precomputed block ranges

The ECC has already created some block ranges to simulate reorgs in
the repository https://github.com/zcash-hackworks/darksidewalletd-test-data.
This may relieve you of the task of generating test blocks. There's a `gRPC` method
called `SetBlocksURL` that takes a resource location (anything that can be
given to `curl`; indeed, the lightwalletd uses `curl`). Here's an example:

`grpcurl -plaintext -d '{"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/blocks-663242-663251"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/SetBlocksURL`

When lightwalletd starts up in darksidewalletd mode, it automatically does the
equivalent of:

`grpcurl -plaintext -d '{"url":"file:testdata/darkside/init-blocks"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/SetBlocksURL`

which is also equivalent to (the `-d @` tells `grpcurl` to read from standard input):
```
cat testdata/darkside/init-blocks |
sed 's/^/{"block":"/;s/$/"}/' |
grpcurl -plaintext -d @ localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/SetBlocks
```

### Simulating a reorg that moves a transaction

First, mine a transaction that receives funds into the developer wallet:
- Stage real mainnet block 663150 (since this is the checkpoint, needs to have the expected block hash)
- Create and stage 100 "synthetic" (manufactured, empty) blocks starting at 663151 (being careful not to overwrite the mainnet block)
- Stage a particular "receive" transaction by its txid
- Apply the staged blocks and transactions and make height 663210 visible

This will do those steps:

```
grpcurl -plaintext -d '{"saplingActivation": 663150,"branchID": "bad", "chainName":"x"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/Reset
grpcurl -plaintext -d '{"url": "https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/basic-reorg/663150.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocks
grpcurl -plaintext -d '{"height":663151,"count":100}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksCreate
grpcurl -plaintext -d '{"height":663190,"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/transactions/recv/0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageTransactions
grpcurl -plaintext -d '{"height":663210}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
```

(Note that it doesn't matter whether you stage transactions before blocks
or the other way around.)

Now check that the transaction is in block 663190:

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

Now, stage that same transaction into a different height, and force a reorg:

- Stage transaction to height 663195
- Create and stage 100 synthetic blocks starting at 663180
- Apply the staged transaction and blocks

This will simulate a reorg back to 663180 (new versions of 663180 and
beyond, same 663179), and the transaction will now be included in 663195
and will _not_ be in 663190. Here are the commands to do this:

```
grpcurl -plaintext -d '{"height":663195,"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/transactions/recv/0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f.txt"}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageTransactions
grpcurl -plaintext -d '{"height":663180,"count":100}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/StageBlocksCreate
grpcurl -plaintext -d '{"height":663210}' localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/ApplyStaged
```

After the `ApplyStaged`, we should see "reorg" messages in the lightwalletd log file. 

Now check that the transaction is no longer in 663190:

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

## Use cases

Check out some of the potential security test cases here: [wallet <->
lightwalletd integration
tests](https://github.com/zcash/lightwalletd/blob/master/docs/integration-tests.md)

## Source Code
* cmd/genblocks -- tool for generating fake block sets.
* testdata/darkside/init-blocks -- the set of blocks loaded by default
* common/darkside.go -- implementation of darksidewalletd
* frontend/service.go -- entrypoints for darksidewalletd GRPC APIs
