#!/usr/bin/env bash
#
# Copyright (c) 2025 The Zcash developers
#
# Run this test with no arguments.
#
# REQUIREMENTS:
# - grpcurl
# - jq

set -e

# Default flag values
help=false
start_server=true

# Parse flags
while getopts "hn" flag; do
  case "$flag" in
    h) help=true ;;
    n) start_server=false ;;
    \?) echo "Invalid option: -$OPTARG" >&2; exit 1 ;;
  esac
done

# Shift past the options to process positional arguments
shift $((OPTIND-1))

# allow the user to just type smoke-test.bash help
test "$1" = help && help=true

# Handle flags
if $help; then
  echo "Usage: $0 [-v] [-h] [-n]"
  echo "  -h  Show this help message"
  echo "  -n  Don't start lightwalletd server (so it can be run from a debugger)"
  exit 0
fi

type -p grpcurl >/dev/null || {
  echo "grpcurl not found"
  echo "you may install grpcurl by running:"
  echo "    go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"
  exit 1
}

type -p jq >/dev/null || {
  echo "jq not found"
  echo "you may install jq by running (on debian, for example):"
  echo "    sudo apt install jq"
  exit 1
}

kill_background_server() {
  echo -n Stopping darkside server ...
  grpcurl -plaintext localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/Stop &>/dev/null
  echo ''
}

if $start_server
then
  trap kill_background_server EXIT
else
  echo not starting server, expecting it to already be running
fi

# grpc production
function gp {
    method=$1; shift
    test $# -gt 0 && set -- -d "$@"
    grpcurl -plaintext "$@" localhost:9067 cash.z.wallet.sdk.rpc.CompactTxStreamer/$method
}

# grpc test (darksidewallet)
function gt {
    method=$1; shift
    test $# -gt 0 && set -- -d "$@"
    grpcurl -plaintext "$@" localhost:9067 cash.z.wallet.sdk.rpc.DarksideStreamer/$method
}

function wait_height {
  i=0
  while :
  do
    h=$(gp GetLightdInfo | jq .blockHeight)
    h=${h//\"}
    test $h -ge $1 && break
    let i=$i+1
    test $i -gt 10 && { echo cannot reach height $1;exit 1;}
    sleep 1
  done
}

function compare {
  expected="$1"
  actual="$2"
  if test "$expected" != "$actual"
  then
    echo
    echo FAILURE --------------------------------------
    echo expected: "$expected"
    echo
    echo actual: "$actual"
    echo
    echo diff "(expected, actual)":
    echo "$expected" > /tmp/expected.$$
    echo "$actual" | diff /tmp/expected.$$ -
    rm /tmp/expected.$$
    exit 1
  fi
}

if $start_server
then
  echo starting the server, this takes a few seconds ...
  go run main.go --donation-address udonationaddr --zcash-conf-path ~/.zcash/zcash.conf --no-tls-very-insecure --darkside-timeout 999999 --darkside-very-insecure &
  sleep 8
fi

# Most of this test comes from docs/darksidewalletd.md "Simulating a reorg that moves a transaction"
echo -n test: Reset ...
gt Reset '{"saplingActivation": 663150,"branchID": "bad", "chainName":"x"}'
echo -n test: StageBlocks block 663150 ...
gt StageBlocks '{"url": "https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/basic-reorg/663150.txt"}'
echo -n test: StageBlocksCreate blocks 663151 to 663250 ...
gt StageBlocksCreate '{"height":663151,"count":100}'
# add a transaction
echo -n test: StageTransactions to block 663190 ...
gt StageTransactions '{"height":663190,"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/transactions/recv/0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f.txt"}'
echo -n test: ApplyStaged to block 663210 ...
gt ApplyStaged '{"height":663210}'

# the block ingestor needs time to receive the block from the (fake) zcashd
wait_height 663190

# The transaction in this block is on mainnet, but in block 663229.
# Its txid is 0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f
# This transaction has one shielded input and two shielded outputs, no actions,
# and zero transparent ins or outs
echo Getblock 663190 ...
actual=$(gp GetBlock '{"height":663190}')
expected='{
  "height": "663190",
  "hash": "rnAgBry+ZWhKw+LSrWfMDFuLalVQVq7u/QyJAPFae1Y=",
  "prevHash": "xOcqS6kNnE4yHGnLcvi1LMOqh9iY3ynEGpUTraSKpvQ=",
  "time": 1,
  "vtx": [
    {
      "txid": "jZno30toIVWj7Np39zJPgvdkZnTAGn4L8Mgcn8t79zo=",
      "vout": [
        {
          "value": "500100000",
          "scriptPubKey": "dqkUftFZRuwUrgzY+omR62CERS6z93yIrA=="
        },
        {
          "value": "125000000",
          "scriptPubKey": "qRTkRc+pRLbyvazvvakEqB1f3SbXf4c="
        }
      ]
    },
    {
      "index": "1",
      "txid": "H0nPz83r1cuQhdn/LvvNqHEh3aE/LHkRE/zy55uoIQg=",
      "spends": [
        {
          "nf": "xrZLCu+Kbv6PXo8cqM+f25Hp55L2cm95bM68JwUnDHg="
        }
      ],
      "outputs": [
        {
          "cmu": "pe/G9q13FyE6vAhrTPzIGpU5Dht5DvJTuc9zmTEx0gU=",
          "ephemeralKey": "qw5MPsRoe8aOnvZ/VB3r1Ja/WkHb52TVU1vyHjGEOqc=",
          "ciphertext": "R2uN3CHagj7Oo+6O9VeBrE6x4dQ07Jl18rVM27vGhl1Io75lFYCHA1SrV72Zu+bgwMilTA=="
        },
        {
          "cmu": "3rQ9DMmk7RaWGf9q0uOYQ7FieHL/TE8Z+QCcS/IJfkA=",
          "ephemeralKey": "U1NCOlTzIF1qlprAjuGUUj591GpO5Vs5WTsmCW35Pio=",
          "ciphertext": "2MbBHjPbkDT/GVsXgDHhihFQizxvizHINXKVbXKnv3Ih1P4c1f3By+TLH2g1yAG3lSARuQ=="
        }
      ]
    }
  ],
  "chainMetadata": {
    "saplingCommitmentTreeSize": 2
  }
}'
compare "$expected" "$actual"

# force a reorg with the transactions now in 663195
echo -n test: StageBlocksCreate blocks 663180 to 663179 ...
gt StageBlocksCreate '{"height":663180,"count":100}'
echo -n test: StageTransactions block 663195 ...
gt StageTransactions '{"height":663195,"url":"https://raw.githubusercontent.com/zcash-hackworks/darksidewalletd-test-data/master/transactions/recv/0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f.txt"}'

# The first two bytes of 0821...cf491f (big-endian, see above) are 08 and 21; specifying
# 0821 as the exclude filter should cause the transaction with that txid to NOT be returned.
# 0821 converted to base64 (which grpcurl expects for binary data), is IQg=

echo GetMempoolTx no txid filter, default shielded only...
actual=$(gp GetMempoolTx | jq -s '.|length')
expected='1'
compare "$expected" "$actual"

echo GetMempoolTx with 2-byte matching filter, should exclude the one shielded tx...
actual=$(gp GetMempoolTx '{"exclude_txid_suffixes":["IQg="]}' | jq -s '.|length')
expected='0'
compare "$expected" "$actual"

echo GetMempoolTx no txid filter, transparent only...
actual=$(gp GetMempoolTx '{"poolTypes":["TRANSPARENT"]}' | jq -s '.|length')
expected='100'
compare "$expected" "$actual"

echo GetMempoolTx with 2-byte filter matching the shielded tx, transparent only...
actual=$(gp GetMempoolTx '{"poolTypes":["TRANSPARENT"], "exclude_txid_suffixes":["IQg="]}' | jq -s '.|length')
expected='100'
compare "$expected" "$actual"

# Should also work with a 3-byte filter, 0821a8. Convert to base64 for the argument.
echo GetMempoolTx with 3-byte matching filter ...
actual=$(gp GetMempoolTx '{"poolTypes":["TRANSPARENT"], "exclude_txid_suffixes":["qCEI"]}' | jq -s '.|length')
expected='100'
compare "$expected" "$actual"

# Any other filter should cause the entry to be returned (no exclude match).
# So the shielded transaction should be return (one more tx than above).
echo GetMempoolTx with unmatched filter...
actual=$(gp GetMempoolTx '{"poolTypes":["TRANSPARENT", "SAPLING"], "exclude_txid_suffixes":["SR8="]}' | jq -s '.|length')
expected='101'
compare "$expected" "$actual"

echo -n test: ApplyStaged to block 663210 ...
gt ApplyStaged '{"height":663210}'
# hack: we can't just wait_height here because 663190 will exist immediately
sleep 4
echo GetBlock 663190 - transaction should no longer exist ...
actual=$(gp GetBlock '{"height":663190}')
expected='{
  "height": "663190",
  "hash": "BaN41+Qi/8MY9vrJxDD1gPde1uGt4d3FTeXwhJaqea8=",
  "prevHash": "xOcqS6kNnE4yHGnLcvi1LMOqh9iY3ynEGpUTraSKpvQ=",
  "time": 1,
  "vtx": [
    {
      "txid": "jZno30toIVWj7Np39zJPgvdkZnTAGn4L8Mgcn8t79zo=",
      "vout": [
        {
          "value": "500100000",
          "scriptPubKey": "dqkUftFZRuwUrgzY+omR62CERS6z93yIrA=="
        },
        {
          "value": "125000000",
          "scriptPubKey": "qRTkRc+pRLbyvazvvakEqB1f3SbXf4c="
        }
      ]
    }
  ],
  "chainMetadata": {}
}'
compare "$expected" "$actual"

echo GetBlock 663195 - transaction has moved to this block ...
actual=$(gp GetBlock '{"height":663195}')
expected='{
  "height": "663195",
  "hash": "l5Lmrtysk2AXXLlM3BO5+hkTDPRA6jFUCc8P2FLZKNE=",
  "prevHash": "gJUabqKu3i1XfWeKHRveM8eyNYXB9e/W6ndgi3d9ntA=",
  "time": 1,
  "vtx": [
    {
      "txid": "k6QFCtomiH4+XGaUhij/QXwGM5/k0KgvEZbwa/oJ5VI=",
      "vout": [
        {
          "value": "500100000",
          "scriptPubKey": "dqkUftFZRuwUrgzY+omR62CERS6z93yIrA=="
        },
        {
          "value": "125000000",
          "scriptPubKey": "qRTkRc+pRLbyvazvvakEqB1f3SbXf4c="
        }
      ]
    },
    {
      "index": "1",
      "txid": "H0nPz83r1cuQhdn/LvvNqHEh3aE/LHkRE/zy55uoIQg=",
      "spends": [
        {
          "nf": "xrZLCu+Kbv6PXo8cqM+f25Hp55L2cm95bM68JwUnDHg="
        }
      ],
      "outputs": [
        {
          "cmu": "pe/G9q13FyE6vAhrTPzIGpU5Dht5DvJTuc9zmTEx0gU=",
          "ephemeralKey": "qw5MPsRoe8aOnvZ/VB3r1Ja/WkHb52TVU1vyHjGEOqc=",
          "ciphertext": "R2uN3CHagj7Oo+6O9VeBrE6x4dQ07Jl18rVM27vGhl1Io75lFYCHA1SrV72Zu+bgwMilTA=="
        },
        {
          "cmu": "3rQ9DMmk7RaWGf9q0uOYQ7FieHL/TE8Z+QCcS/IJfkA=",
          "ephemeralKey": "U1NCOlTzIF1qlprAjuGUUj591GpO5Vs5WTsmCW35Pio=",
          "ciphertext": "2MbBHjPbkDT/GVsXgDHhihFQizxvizHINXKVbXKnv3Ih1P4c1f3By+TLH2g1yAG3lSARuQ=="
        }
      ]
    }
  ],
  "chainMetadata": {
    "saplingCommitmentTreeSize": 2
  }
}'
compare "$expected" "$actual"

echo GetLatestBlock - height should be 663210 ...
actual=$(gp GetLatestBlock)
expected='{
  "height": "663210",
  "hash": "2PP5ywTsJmEu5Uf4wR2f8cPiZOcq7bATKGkEW2oP43Y="
}'
compare "$expected" "$actual"

echo -n ApplyStaged 663220 ...
gt ApplyStaged '{"height":663220}'
wait_height 663220

echo GetLatestBlock - height should be 663220 ...
actual=$(gp GetLatestBlock)
expected='{
  "height": "663220",
  "hash": "y4EFJMCjxlTT76DhwRrqqsL6+l0aYXyg0nIUr2/q0x4="
}'
compare "$expected" "$actual"

echo GetLightdInfo ...
actual=$(gp GetLightdInfo)
expected='{
  "version": "v0.0.0.0-dev",
  "vendor": "ECC DarksideWalletD",
  "taddrSupport": true,
  "chainName": "x",
  "saplingActivationHeight": "663150",
  "consensusBranchId": "bad",
  "blockHeight": "663220",
  "zcashdBuild": "darksidewallet-build",
  "zcashdSubversion": "darksidewallet-subversion",
  "donationAddress": "udonationaddr"
}'
compare "$expected" "$actual"

echo GetBlockRange 663152 to 663154 ...
actual=$(gp GetBlockRange '{"poolTypes": ["TRANSPARENT"], "start":{"height":663152},"end":{"height":663154}}')
expected='{
  "height": "663152",
  "hash": "uzuBbqy3JKKpssnPJHLXLq+nv1eTHsuyQAkYiR84y7M=",
  "prevHash": "oh3nSTQCTZgnRGBgS8rEZt/cyjjxMI78X49lkTXJyBc=",
  "time": 1,
  "vtx": [
    {
      "txid": "+qNwLv47uukw7U1Ge1x7p1Ym2SrQ67z5SgahxefXxIs=",
      "vout": [
        {
          "value": "500100000",
          "scriptPubKey": "dqkUftFZRuwUrgzY+omR62CERS6z93yIrA=="
        },
        {
          "value": "125000000",
          "scriptPubKey": "qRTkRc+pRLbyvazvvakEqB1f3SbXf4c="
        }
      ]
    }
  ],
  "chainMetadata": {}
}
{
  "height": "663153",
  "hash": "BQpVcT2DPoC71Oo1DyL4arAeXWFEMDsOQfwsObbKY4s=",
  "prevHash": "uzuBbqy3JKKpssnPJHLXLq+nv1eTHsuyQAkYiR84y7M=",
  "time": 1,
  "vtx": [
    {
      "txid": "v8CDZ8SiXuyV60eFHNGqnEwQhsR1761v3djLh5MaRBg=",
      "vout": [
        {
          "value": "500100000",
          "scriptPubKey": "dqkUftFZRuwUrgzY+omR62CERS6z93yIrA=="
        },
        {
          "value": "125000000",
          "scriptPubKey": "qRTkRc+pRLbyvazvvakEqB1f3SbXf4c="
        }
      ]
    }
  ],
  "chainMetadata": {}
}
{
  "height": "663154",
  "hash": "5KcOajo6RLRL8ZKgdALls72ByFgTKE4zJ9kbzBh/k1I=",
  "prevHash": "EneA7vMz88tX2BvMT6UhNd+DMOSVWNVurPyEJZO/IkU=",
  "time": 1,
  "vtx": [
    {
      "txid": "P4MhIyT2ziuCKfejzrBlWwl/wegqR763c4vzqxqZlWQ=",
      "vout": [
        {
          "value": "500100000",
          "scriptPubKey": "dqkUftFZRuwUrgzY+omR62CERS6z93yIrA=="
        },
        {
          "value": "125000000",
          "scriptPubKey": "qRTkRc+pRLbyvazvvakEqB1f3SbXf4c="
        }
      ]
    }
  ],
  "chainMetadata": {}
}'
compare "$expected" "$actual"

echo GetTransaction ...
actual=$(gp GetTransaction '{"hash": "H0nPz83r1cuQhdn/LvvNqHEh3aE/LHkRE/zy55uoIQg="}')
expected='{
  "data": "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",
  "height": "663195"
}'
compare "$expected" "$actual"

echo SendTransaction ...
actual=$(gp SendTransaction "$actual")
expected='{
  "errorMessage": "0821a89be7f2fc1311792c3fa1dd2171a8cdfb2effd98590cbd5ebcdcfcf491f"
}'
compare "$expected" "$actual"
