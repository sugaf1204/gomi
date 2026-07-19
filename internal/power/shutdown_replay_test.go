package power

import (
	"testing"
	"time"
)

func TestShutdownReplayCacheRejectsReplayedNonce(t *testing.T) {
	ttl := 60 * time.Second
	cache := NewShutdownReplayCache(ttl)
	now := time.Now().UTC()
	nonce := []byte("nonce-000001")

	if cache.Seen(nonce, now) {
		t.Fatal("first use of a nonce must be accepted")
	}
	if !cache.Seen(nonce, now.Add(time.Second)) {
		t.Fatal("replayed nonce within the retention window must be rejected")
	}
	if !cache.Seen(nonce, now.Add(2*ttl-time.Second)) {
		t.Fatal("replayed nonce at the edge of the retention window must be rejected")
	}
	if cache.Seen([]byte("nonce-000002"), now) {
		t.Fatal("a different nonce must be accepted")
	}
}

func TestShutdownReplayCacheEvictsExpiredNonces(t *testing.T) {
	ttl := 60 * time.Second
	cache := NewShutdownReplayCache(ttl)
	now := time.Now().UTC()
	nonce := []byte("nonce-expired")

	if cache.Seen(nonce, now) {
		t.Fatal("first use of a nonce must be accepted")
	}
	// Beyond the retention window the timestamp check rejects the packet
	// anyway, so the cache may forget the nonce and accept it again.
	if cache.Seen(nonce, now.Add(2*ttl+time.Second)) {
		t.Fatal("expired nonce should be evicted and accepted again")
	}
}
