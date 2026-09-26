package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func GenerateServiceAccountToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "gomi_sa_" + hex.EncodeToString(value), nil
}
func ServiceAccountTokenHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
