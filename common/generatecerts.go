// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

// GenerateCerts create self signed certificate for local development use
// (and, if using grpcurl, specify the -insecure argument option)
func GenerateCerts() *tls.Certificate {

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	publicKey := &privKey.PublicKey

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		Log.Fatal("Failed to generate serial number:", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Lighwalletd developer"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Local().Add(time.Hour * 24 * 365),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// List of hostnames and IPs for the cert
	template.DNSNames = append(template.DNSNames, "localhost")

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey, privKey)
	if err != nil {
		Log.Fatal("Failed to create certificate:", err)
	}

	// PEM encode the certificate (this is a standard TLS encoding)
	b := pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	certPEM := pem.EncodeToMemory(&b)

	// PEM encode the private key
	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		Log.Fatal("Unable to marshal private key:", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: privBytes,
	})

	// Create a TLS cert using the private key and certificate
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		Log.Fatal("invalid key pair:", err)
	}

	return &tlsCert
}
