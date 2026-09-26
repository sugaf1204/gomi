// Package sealedseed encrypts physical-machine bootstrap for an enrolled host.
// The private RSA host key stays on that host throughout OS reinstallation.
package sealedseed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
)

const Algorithm = "RSA-OAEP-SHA256+A256GCM"
const MaxPlaintext = 4 << 20

type Envelope struct {
	Version        int    `json:"version"`
	Algorithm      string `json:"algorithm"`
	Host           string `json:"host"`
	Owner          string `json:"owner"`
	KeyFingerprint string `json:"keyFingerprint"`
	WrappedKey     string `json:"wrappedKey"`
	Nonce          string `json:"nonce"`
	Ciphertext     string `json:"ciphertext"`
}

func contextData(host, owner string) []byte {
	return []byte("gomi-capi-bootstrap/v1\x00" + host + "\x00" + owner)
}

// Seal accepts a PEM SubjectPublicKeyInfo RSA key, obtained through trusted
// enrollment, never from an unauthenticated PXE request. Encryption protects
// bootstrap confidentiality; it does not make an unsigned PXE boot trustworthy.
func Seal(publicKeyPEM, host, owner string, plaintext []byte) ([]byte, error) {
	if host == "" || !strings.HasPrefix(owner, "capi-") || strings.ContainsAny(host+owner, "\x00\r\n") {
		return nil, fmt.Errorf("host and claim owner are required")
	}
	if len(plaintext) == 0 || len(plaintext) > MaxPlaintext {
		return nil, fmt.Errorf("invalid bootstrap size")
	}
	block, rest := pem.Decode([]byte(publicKeyPEM))
	if block == nil || block.Type != "PUBLIC KEY" || strings.TrimSpace(string(rest)) != "" {
		return nil, fmt.Errorf("expected one PEM public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid enrollment public key")
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("enrollment requires RSA with at least 2048 bits")
	}
	symmetric := make([]byte, 32)
	if _, err = rand.Read(symmetric); err != nil {
		return nil, err
	}
	defer func() {
		for i := range symmetric {
			symmetric[i] = 0
		}
	}()
	blockCipher, err := aes.NewCipher(symmetric)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	label := contextData(host, owner)
	wrapped, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, symmetric, label)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(block.Bytes)
	encode := base64.StdEncoding.EncodeToString
	return json.Marshal(Envelope{Version: 1, Algorithm: Algorithm, Host: host, Owner: owner, KeyFingerprint: hex.EncodeToString(fingerprint[:]), WrappedKey: encode(wrapped), Nonce: encode(nonce), Ciphertext: encode(gcm.Seal(nil, nonce, plaintext, label))})
}
