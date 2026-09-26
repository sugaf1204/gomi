package baremetal

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
)

// EnrollmentFingerprint validates a trusted, per-host RSA public key. Private
// keys are never registered or returned by the GOMI allocation API.
func EnrollmentFingerprint(value string) (string, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || block.Type != "PUBLIC KEY" || strings.TrimSpace(string(rest)) != "" {
		return "", fmt.Errorf("one PEM public key is required")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("invalid public key")
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < 2048 {
		return "", fmt.Errorf("RSA public key of at least 2048 bits is required")
	}
	digest := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(digest[:]), nil
}
