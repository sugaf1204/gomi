package power

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// ShutdownPacket represents a parsed WoL shutdown request.
type ShutdownPacket struct {
	MachineName string
	Token       string
	Timestamp   time.Time
	Nonce       []byte
}

func (p ShutdownPacket) RequestID() string {
	return hex.EncodeToString(p.Nonce)
}

// ParseAndVerifyShutdownPacket decodes the binary shutdown packet,
// verifies the HMAC-SHA256 signature, and checks timestamp freshness.
func ParseAndVerifyShutdownPacket(data []byte, secret string, now time.Time, ttl time.Duration) (ShutdownPacket, error) {
	const (
		magicLen     = 4
		timestampLen = 8
		nonceLen     = 12
		sigLen       = 32 // HMAC-SHA256
		headerLen    = magicLen + timestampLen + nonceLen
	)

	if len(data) < headerLen+2+sigLen {
		return ShutdownPacket{}, fmt.Errorf("packet too short")
	}

	if string(data[:magicLen]) != "GOMI" {
		return ShutdownPacket{}, fmt.Errorf("invalid magic")
	}

	tsRaw := binary.BigEndian.Uint64(data[magicLen : magicLen+timestampLen])
	ts := time.Unix(int64(tsRaw), 0)
	nonce := data[magicLen+timestampLen : headerLen]

	pos := headerLen
	nameLen := int(data[pos])
	pos++
	if pos+nameLen > len(data)-sigLen {
		return ShutdownPacket{}, fmt.Errorf("invalid name length")
	}
	machineName := string(data[pos : pos+nameLen])
	pos += nameLen

	tokenLen := int(data[pos])
	pos++
	if pos+tokenLen > len(data)-sigLen {
		return ShutdownPacket{}, fmt.Errorf("invalid token length")
	}
	token := string(data[pos : pos+tokenLen])
	pos += tokenLen

	body := data[:pos]
	sig := data[pos : pos+sigLen]

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(body); err != nil {
		return ShutdownPacket{}, err
	}
	if !hmac.Equal(mac.Sum(nil), sig) {
		return ShutdownPacket{}, fmt.Errorf("invalid signature")
	}

	const clockSkewAllowance = 5 * time.Second
	diff := now.Sub(ts)
	if diff < -clockSkewAllowance || diff > ttl {
		return ShutdownPacket{}, fmt.Errorf("timestamp outside ttl window")
	}

	return ShutdownPacket{
		MachineName: machineName,
		Token:       token,
		Timestamp:   ts,
		Nonce:       nonce,
	}, nil
}
