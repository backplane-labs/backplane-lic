package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// MachineProof is Machine#generate_proof: "proof/<data>.<sig>", the
// dataset JSON and its RSA-SHA256 PKCS#1 v1.5 signature over
// "proof/<data>", both URL-safe base64 with padding.
func MachineProof(keys AccountKeys, dataset []byte) (string, error) {
	priv, err := parseRSAPrivate(keys.RSAPrivate)
	if err != nil {
		return "", err
	}

	signing := "proof/" + base64.URLEncoding.EncodeToString(dataset)
	digest := sha256.Sum256([]byte(signing))

	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("crypto: machine proof: %w", err)
	}

	return signing + "." + base64.URLEncoding.EncodeToString(sig), nil
}
