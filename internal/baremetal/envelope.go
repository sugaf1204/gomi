package baremetal

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// ValidateEnvelope validates routing and bounds, without decrypting bootstrap.
func ValidateEnvelope(raw []byte, h Host) error {
	if len(raw) == 0 || len(raw) > 6<<20 {
		return fmt.Errorf("invalid sealed bootstrap size")
	}
	var envelope struct {
		Version        int    `json:"version"`
		Algorithm      string `json:"algorithm"`
		Host           string `json:"host"`
		Owner          string `json:"owner"`
		KeyFingerprint string `json:"keyFingerprint"`
		WrappedKey     string `json:"wrappedKey"`
		Nonce          string `json:"nonce"`
		Ciphertext     string `json:"ciphertext"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("invalid sealed bootstrap envelope")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing bootstrap data")
	}
	fingerprint, err := EnrollmentFingerprint(h.PublicKey)
	if err != nil {
		return err
	}
	if envelope.Version != 1 || envelope.Algorithm != "RSA-OAEP-SHA256+A256GCM" || envelope.Host != h.Name || envelope.Owner != h.Owner || envelope.KeyFingerprint != fingerprint {
		return fmt.Errorf("bootstrap enrollment identity mismatch")
	}
	decode := base64.StdEncoding.Strict().DecodeString
	wrapped, e1 := decode(envelope.WrappedKey)
	nonce, e2 := decode(envelope.Nonce)
	ciphertext, e3 := decode(envelope.Ciphertext)
	if e1 != nil || e2 != nil || e3 != nil || len(wrapped) < 256 || len(wrapped) > 2048 || len(nonce) != 12 || len(ciphertext) <= 16 || len(ciphertext) > (4<<20)+16 {
		return fmt.Errorf("invalid sealed bootstrap encoding")
	}
	return nil
}
