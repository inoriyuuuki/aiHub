package core

import (
	"sync"
	"time"
)

// maxRateKeys bounds the in-memory attempt map to prevent memory exhaustion.
const maxRateKeys = 100000

// rateLimiter is a simple in-memory sliding-window limiter keyed by address.
type rateLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, attempts: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it is permitted.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	recent := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.attempts[key] = recent
		return false
	}
	// Opportunistic sweep: keep the map bounded.
	if len(l.attempts) >= maxRateKeys {
		for k, times := range l.attempts {
			cut := times[:0]
			for _, t := range times {
				if t.After(cutoff) {
					cut = append(cut, t)
				}
			}
			if len(cut) == 0 {
				delete(l.attempts, k)
			} else {
				l.attempts[k] = cut
			}
		}
	}
	l.attempts[key] = append(recent, now)
	return true
}

// reset clears attempts for a key (e.g. after successful login).
func (l *rateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
