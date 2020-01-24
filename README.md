
[![pipeline status](https://gitlab.com/zcash/lightwalletd/badges/master/pipeline.svg)](https://gitlab.com/zcash/lightwalletd/commits/master)
[![codecov](https://codecov.io/gh/zcash/lightwalletd/branch/master/graph/badge.svg)](https://codecov.io/gh/zcash/lightwalletd)

# Disclaimer
This is an alpha build and is currently under active development. Please be advised of the following:

- This code currently is not audited by an external security auditor, use it at your own risk
- The code **has not been subjected to thorough review** by engineers at the Electric Coin Company
- We **are actively changing** the codebase and adding features where/when needed

🔒 Security Warnings

The Lightwalletd Server is experimental and a work in progress. Use it at your own risk.

---

# Overview

[lightwalletd](https://github.com/zcash-hackworks/lightwalletd) is a backend service that provides a bandwidth-efficient interface to the Zcash blockchain. Currently, lightwalletd supports the Sapling protocol version as its primary concern. The intended purpose of lightwalletd is to support the development of mobile-friendly shielded light wallets.

lightwalletd is a backend service that provides a bandwidth-efficient interface to the Zcash blockchain for mobile and other wallets, such as [Zecwallet](https://github.com/adityapk00/zecwallet-lite-lib).

Lightwalletd has not yet undergone audits or been subject to rigorous testing. It lacks some affordances necessary for production-level reliability. We do not recommend using it to handle customer funds at this time (October 2019).

To view status of [CI pipeline](https://gitlab.com/mdr0id/lightwalletd/pipelines)

To view detailed [Codecov](https://codecov.io/gh/zcash-hackworks/lightwalletd) report

# Local/Developer docker-compose Usage

[docs/docker-compose-setup.md](./docs/docker-compose-setup.md)

# Local/Developer Usage

First, ensure [Go >= 1.11](https://golang.org/dl/#stable) is installed. Once your go environment is setup correctly, you can build/run the below components.

To build the server, run `make`.

This will build the server binary, where you can use the below commands to configure how it runs.

## To run SERVER

Assuming you used `make` to build SERVER:

```
./server --no-tls-very-insecure=true --conf-file /home/zcash/.zcash/zcash.conf --log-file /logs/server.log --bind-addr 127.0.0.1:18232
```

# Production Usage

Ensure [Go >= 1.11](https://golang.org/dl/#stable) is installed.

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
`gofmt` command as indicated and rerun the `git commit` command.
