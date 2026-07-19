package power

import "time"

// ShutdownReplayCache rejects reuse of shutdown packet nonces within the
// acceptance window. The freshness check tolerates clock skew up to the ttl in
// both directions, so without nonce tracking a captured packet could be
// replayed for almost twice the ttl; with it a packet triggers at most one
// shutdown.
type ShutdownReplayCache struct {
	retention time.Duration
	seen      map[string]time.Time
}

// NewShutdownReplayCache creates a cache that remembers nonces for twice the
// packet ttl, covering the full timestamp acceptance window.
func NewShutdownReplayCache(ttl time.Duration) *ShutdownReplayCache {
	return &ShutdownReplayCache{retention: 2 * ttl, seen: map[string]time.Time{}}
}

// Seen records the nonce and reports whether it was already used within the
// retention window. Expired entries are evicted on each call; the map stays
// small because packets are rare and short-lived.
func (c *ShutdownReplayCache) Seen(nonce []byte, now time.Time) bool {
	for k, t := range c.seen {
		if now.Sub(t) > c.retention {
			delete(c.seen, k)
		}
	}
	key := string(nonce)
	if _, ok := c.seen[key]; ok {
		return true
	}
	c.seen[key] = now
	return false
}
