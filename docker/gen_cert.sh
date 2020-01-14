#!/bin/bash

## In debian
# apt-get update && apt-get install -y openssl

openssl req -x509 -nodes -newkey rsa:2048 -keyout ./cert.key -out ./cert.pem -days 3650 -subj "/C=US/ST=CA/O=MyOrg, Inc./CN=lightwalletd.testnet.local" -reqexts SAN -config <(cat /etc/ssl/openssl.cnf <(printf "\n[SAN]\nsubjectAltName=DNS:lightwalletd.testnet.local,DNS:127.0.0.1,DNS:localhost"))