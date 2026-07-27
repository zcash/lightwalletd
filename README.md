
[![pipeline status](https://gitlab.com/zcash/lightwalletd/badges/master/pipeline.svg)](https://gitlab.com/zcash/lightwalletd/commits/master)
[![codecov](https://codecov.io/gh/zcash/lightwalletd/branch/master/graph/badge.svg)](https://codecov.io/gh/zcash/lightwalletd)

# Security Disclaimer

lightwalletd is under active development, some features are more stable than
others. The code has not been subjected to a thorough review by an external
auditor, and recent code changes have not yet received security review from
Electric Coin Company's security team.

Developers should familiarize themselves with the [wallet app threat
model](https://zcash.readthedocs.io/en/latest/rtd_pages/wallet_threat_model.html),
since it contains important information about the security and privacy
limitations of light wallets that use lightwalletd.

---

# Overview

[lightwalletd](https://github.com/zcash/lightwalletd) is a backend service that provides a bandwidth-efficient interface to the Zcash blockchain. Currently, lightwalletd supports the Sapling protocol version and beyond as its primary concern. The intended purpose of lightwalletd is to support the development and operation of mobile-friendly shielded light wallets.

lightwalletd is a backend service that provides a bandwidth-efficient interface to the Zcash blockchain for mobile and other wallets, such as [Zashi](https://github.com/Electric-Coin-Company/zashi-android) and [Ywallet](https://github.com/hhanh00/zwallet).

To view status of [CI pipeline](https://gitlab.com/zcash/lightwalletd/pipelines)

To view detailed [Codecov](https://codecov.io/gh/zcash/lightwalletd) report

Documentation for lightwalletd clients (the gRPC interface) is in `docs/rtd/index.html`. The current version of this file corresponds to the two `.proto` files; if you change these files, please regenerate the documentation by running `make doc`, which requires docker to be installed. 
# Local/Developer docker-compose Usage

[docs/docker-compose-setup.md](./docs/docker-compose-setup.md)

# Local/Developer Usage

## Zcash node

You must have access to a Zcash full node (such as [zebrad](https://zebra.zfnd.org/)
or zakura) with its JSON-RPC interface enabled. For zebrad, the `zebrad.toml` configuration
file must enable the RPC listener, for example:
```toml
[rpc]
listen_addr = "127.0.0.1:8232"
```

Then point lightwalletd at the node's RPC endpoint using the `--rpchost`,
`--rpcport`, `--rpcuser`, and `--rpcpassword` options (all four must be given;
if the node does not check RPC credentials, any values work). Note that the
node's own configuration file (such as `zebrad.toml`) is not a suitable
argument for `--zcash-conf-path`: its RPC address is the node's *bind* address,
which is not necessarily an address that lightwalletd can reach.

The node can be configured to run on mainnet or testnet. If you stop the node and restart it on a different network, you must also stop and restart lightwalletd.

Lightwalletd uses the following node RPCs:
- `getinfo`
- `getblockchaininfo`
- `getbestblockhash`
- `z_gettreestate`
- `getblock`
- `getrawtransaction`
- `sendrawtransaction`
- `getrawmempool`
- `getaddresstxids`
- `getaddressbalance`
- `getaddressutxos`

## Lightwalletd

First, install [Go](https://golang.org/dl/#stable) version 1.17 or later. You can see your current version by running `go version`.

Clone the [current repository](https://github.com/zcash/lightwalletd) into a local directory that is _not_ within any component of
your `$GOPATH` (`$HOME/go` by default), then build the lightwalletd server binary by running `make`.

## To run SERVER

Assuming you used `make` to build the server, here's a typical developer invocation:

```
./lightwalletd --no-tls-very-insecure --rpchost 127.0.0.1 --rpcport 8232 --rpcuser x --rpcpassword x --data-dir . --log-file /dev/stdout
```
Type `./lightwalletd help` to see the full list of options and arguments.

# Production Usage

Run a local Zcash node (see above), except do _not_ specify `--no-tls-very-insecure`.
Ensure [Go](https://golang.org/dl/#stable) version 1.17 or later is installed.

**x509 Certificates**
You will need to supply an x509 certificate that connecting clients will have good reason to trust (hint: do not use a self-signed one, our SDK will reject those unless you distribute them to the client out-of-band). We suggest that you be sure to buy a reputable one from a supplier that uses a modern hashing algorithm (NOT md5 or sha1) and that uses Certificate Transparency (OID 1.3.6.1.4.1.11129.2.4.2 will be present in the certificate).

To check a given certificate's (cert.pem) hashing algorithm:
```
openssl x509 -text -in certificate.crt | grep "Signature Algorithm"
```

To check if a given certificate (cert.pem) contains a Certificate Transparency OID:
```
echo "1.3.6.1.4.1.11129.2.4.2 certTransparency Certificate Transparency" > oid.txt
openssl asn1parse -in cert.pem -oid ./oid.txt | grep 'Certificate Transparency'
```

To use Let's Encrypt to generate a free certificate for your frontend, one method is to:
1) Install certbot
2) Open port 80 to your host
3) Point some forward dns to that host (some.forward.dns.com)
4) Run
```
certbot certonly --standalone --preferred-challenges http -d some.forward.dns.com
```
5) Pass the resulting certificate and key to frontend using the -tls-cert and -tls-key options.

## To run production SERVER

Example using server binary built from Makefile:

```
./lightwalletd --tls-cert cert.pem --tls-key key.pem --rpchost 127.0.0.1 --rpcport 8232 --rpcuser x --rpcpassword x --log-file /logs/server.log
```

## Block cache

lightwalletd caches all blocks, which takes about an hour the first time it runs
lightwalletd. During this syncing, lightwalletd is fully available,
but block fetches are slower until the download completes.

After syncing, lightwalletd will start almost immediately,
because the blocks are cached in local files (by default, within
`/var/lib/lightwalletd/db`; you can specify a different location using
the `--data-dir` command-line option).

lightwalletd checks the consistency of these files at startup and during
operation as these files may be damaged by, for example, an unclean shutdown.
If the server detects corruption, it will automatically re-downloading blocks
from the node from that height, requiring up to an hour again (no manual
intervention is required). But this should occur rarely.

If lightwalletd detects corruption in these cache files, it will log
a message containing the string `CORRUPTION` and also indicate the
nature of the corruption.

## Darksidewalletd & Testing

lightwalletd now supports a mode that enables integration testing of itself and
wallets that connect to it. See the [darksidewalletd
docs](docs/darksidewalletd.md) for more information.

# Pull Requests

We welcome pull requests! We like to keep our Go code neatly formatted in a standard way,
which the standard tool [gofmt](https://golang.org/cmd/gofmt/) can do. Please consider
adding the following to the file `.git/hooks/pre-commit` in your clone:

```
#!/bin/sh

modified_go_files=$(git diff --cached --name-only -- '*.go')
if test "$modified_go_files"
then
    need_formatting=$(gofmt -l $modified_go_files)
    if test "$need_formatting"
    then
        echo files need formatting (then don't forget to git add):
        echo gofmt -w $need_formatting
        exit 1
    fi
fi
```

You'll also need to make this file executable:

```
$ chmod +x .git/hooks/pre-commit
```

Doing this will prevent commits that break the standard formatting. Simply run the
`gofmt` command as indicated and rerun the `git add` and `git commit` commands.
