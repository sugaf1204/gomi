package power

import (
	"testing"
	"time"
)

func TestBuildAndParseShutdownPacket(t *testing.T) {
	secret := "test-hmac-secret"
	machineName := "node-01"
	token := "my-token"
	now := time.Now().UTC()

	packet, err := buildShutdownPacket(machineName, token, secret, now)
	if err != nil {
		t.Fatalf("buildShutdownPacket: %v", err)
	}

	parsed, err := ParseAndVerifyShutdownPacket(packet, secret, now, 60*time.Second)
	if err != nil {
		t.Fatalf("ParseAndVerifyShutdownPacket: %v", err)
	}
	if parsed.MachineName != machineName {
		t.Errorf("MachineName = %q, want %q", parsed.MachineName, machineName)
	}
	if parsed.Token != token {
		t.Errorf("Token = %q, want %q", parsed.Token, token)
	}
	if parsed.Timestamp.Unix() != now.Unix() {
		t.Errorf("Timestamp = %v, want %v", parsed.Timestamp, now)
	}
}

func TestShutdownPacketTamperedSignature(t *testing.T) {
	secret := "test-hmac-secret"
	now := time.Now().UTC()

	packet, err := buildShutdownPacket("node-01", "tok", secret, now)
	if err != nil {
		t.Fatalf("buildShutdownPacket: %v", err)
	}

	// Flip last byte of signature
	packet[len(packet)-1] ^= 0xff

	_, err = ParseAndVerifyShutdownPacket(packet, secret, now, 60*time.Second)
	if err == nil {
		t.Fatal("expected error for tampered signature, got nil")
	}
}

func TestShutdownPacketWrongSecret(t *testing.T) {
	now := time.Now().UTC()
	packet, err := buildShutdownPacket("node-01", "tok", "secret-a", now)
	if err != nil {
		t.Fatalf("buildShutdownPacket: %v", err)
	}

	_, err = ParseAndVerifyShutdownPacket(packet, "secret-b", now, 60*time.Second)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestShutdownPacketExpired(t *testing.T) {
	secret := "test-secret"
	past := time.Now().UTC().Add(-2 * time.Minute)
	packet, err := buildShutdownPacket("node-01", "tok", secret, past)
	if err != nil {
		t.Fatalf("buildShutdownPacket: %v", err)
	}

	_, err = ParseAndVerifyShutdownPacket(packet, secret, time.Now().UTC(), 60*time.Second)
	if err == nil {
		t.Fatal("expected error for expired timestamp, got nil")
	}
}

func TestShutdownPacketClockSkew(t *testing.T) {
	secret := "test-secret"
	// Packet timestamped 3 seconds in the future (within 5s allowance)
	future := time.Now().UTC().Add(3 * time.Second)
	packet, err := buildShutdownPacket("node-01", "tok", secret, future)
	if err != nil {
		t.Fatalf("buildShutdownPacket: %v", err)
	}

	_, err = ParseAndVerifyShutdownPacket(packet, secret, time.Now().UTC(), 60*time.Second)
	if err != nil {
		t.Fatalf("expected success for small clock skew, got: %v", err)
	}
}

func TestShutdownPacketTooShort(t *testing.T) {
	_, err := ParseAndVerifyShutdownPacket([]byte("short"), "secret", time.Now().UTC(), 60*time.Second)
	if err == nil {
		t.Fatal("expected error for short packet, got nil")
	}
}

func TestShutdownPacketInvalidMagic(t *testing.T) {
	data := make([]byte, 60)
	copy(data[:4], "NOPE")
	_, err := ParseAndVerifyShutdownPacket(data, "secret", time.Now().UTC(), 60*time.Second)
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}
}
