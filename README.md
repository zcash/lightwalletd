
[![pipeline status](https://gitlab.com/zcash/lightwalletd/badges/master/pipeline.svg)](https://gitlab.com/zcash/lightwalletd/commits/master)
[![coverage report](https://gitlab.com/zcash/lightwalletd/badges/master/coverage.svg)](https://gitlab.com/zcash/lightwalletd/commits/master)

# Disclaimer
This is an alpha build and is currently under active development. Please be advised of the following:

- This code currently is not audited by an external security auditor, use it at your own risk
- The code **has not been subjected to thorough review** by engineers at the Electric Coin Company
- We **are actively changing** the codebase and adding features where/when needed

🔒 Security Warnings

The Lightwalletd Server is experimental and a work in progress. Use it at your own risk.

---

# Overview

[lightwalletd](https://github.com/zcash/lightwalletd) is a backend service that provides a bandwidth-efficient interface to the Zcash blockchain. Currently, lightwalletd supports the Sapling protocol version as its primary concern. The intended purpose of lightwalletd is to support the development of mobile-friendly shielded light wallets.

lightwalletd is a backend service that provides a bandwidth-efficient interface to the Zcash blockchain for mobile and other wallets, such as [Zecwallet](https://github.com/adityapk00/zecwallet-lite-lib).

Lightwalletd has not yet undergone audits or been subject to rigorous testing. It lacks some affordances necessary for production-level reliability. We do not recommend using it to handle customer funds at this time (October 2019).

To view status of [CI pipeline](https://gitlab.com/mdr0id/lightwalletd/pipelines)

To view detailed [Codecov](https://codecov.io/gh/zcash/lightwalletd) report

Documentation for lightwalletd clients (the gRPC interface) is in `docs/rtd/index.html`. The current version of this file corresponds to the two `.proto` files; if you change these files, please regenerate the documentation by running `make doc`, which requires docker to be installed. 
# Local/Developer docker-compose Usage

[docs/docker-compose-setup.md](./docs/docker-compose-setup.md)

# Local/Developer Usage

## Zcashd

You must start a local instance of `zcashd`, and its `.zcash/zcash.conf` file must include the following entries:
```
txindex=1
insightexplorer=1
experimentalfeatures=1
```

It's necessary to run `zcashd --reindex` one time for these options to take effect. This typically takes several hours, and requires more space in the `.zcash` data directory.

Lightwalletd uses the following `zcashd` RPCs:
- `getblockchaininfo`
- `getblock`
- `getrawtransaction`
- `getaddresstxids`
- `sendrawtransaction`

## Lightwalletd

First, install [Go](https://golang.org/dl/#stable) version 1.11 or later. You can see your current version by running `go version`.

To build the server, run `make`.

This will build the server binary, where you can use the below commands to configure how it runs.

## To run SERVER

Assuming you used `make` to build SERVER:

```
./server --no-tls-very-insecure=true --conf-file /home/zcash/.zcash/zcash.conf --log-file /logs/server.log --bind-addr 127.0.0.1:18232
```

# Production Usage

Run a local instance of `zcashd` (see above).
Ensure [Go](https://golang.org/dl/#stable) version 1.11 or later is installed.

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
./server --tls-cert cert.pem --tls-key key.pem --conf-file /home/zcash/.zcash/zcash.conf --log-file /logs/server.log --bind-addr 127.0.0.1:18232
```

## Block cache

Lightwalletd caches all blocks from Sapling activation up to the
most recent block, which takes about an hour the first time you run
lightwalletd. During this syncing, lightwalletd is fully available; the
only effect of being in download mode is that block fetches are slower.

After syncing, lightwalletd will start almost immediately,
because the blocks are cached in local files (by default, within
`/var/lib/lightwalletd/db`; you can specify a different location using
the `--data-dir` command-line option).

Lightwalletd checks the consistency of these files at startup and during
operation, as might be caused by an unclean shutdown, and if it detects
corruption, it will recreate the cache by re-downloading all blocks
from `zcashd` requiring an hour again, but this should occur extremely
rarely.

If lightwalletd detects corruption in these cache files, it will log
a message containing the string `CORRUPTION` and also indicate the
nature of the corruption.


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
        echo files need formatting:
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
