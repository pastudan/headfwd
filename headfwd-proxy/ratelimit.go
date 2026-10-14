package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Token-bucket rate limiting for the registration endpoints.
//
// Registration is unauthenticated by design — anyone may register a subdomain
// they can prove they hold the key for — so the only thing standing between the
// relay and an abusive client is this. Both endpoints do real crypto work
// (X25519 keygen on init, ECDH + HKDF on verify), so unbounded requests are a
// cheap way to burn CPU.
//
// Deliberately in-memory and per-instance. A shared store would be more precise
// across a multi-region deployment, but it would also add a dependency and a
// failure mode to a service whose whole appeal is that it is one static binary.
// Per-instance limits are enough to stop the abuse that matters.

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	// rate is tokens added per second; burst is the ceiling.
	rate  float64
	burst float64
}

func newRateLimiter(perMinute, burst int) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
	}
	go rl.reap()
	return rl
}

// allow consumes a token for key, reporting whether the request may proceed.
func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		// A new client starts with a full bucket minus this request, so a
		// one-off registration never waits.
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, lastSeen: now}
		return true
	}

	b.tokens += now.Sub(b.lastSeen).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// reap drops idle buckets so the map cannot grow without bound. Without this,
// an attacker cycling source addresses turns the limiter itself into the leak.
func (rl *rateLimiter) reap() {
	for range time.Tick(5 * time.Minute) {
		cutoff := time.Now().Add(-10 * time.Minute)
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP extracts the caller's address for rate-limiting purposes.
//
// Fly terminates TLS and forwards the real address in Fly-Client-IP, so
// RemoteAddr alone would bucket every request behind the proxy together and
// rate-limit the entire internet as one client. X-Forwarded-For is honoured as
// a fallback, taking the FIRST entry (the original client; later entries are
// appended by intermediaries).
//
// Both headers are attacker-controllable if the relay is ever exposed without a
// trusted proxy in front. That is acceptable here: the worst case is an
// attacker evading their own rate limit, which is no worse than having none,
// and the alternative (trusting RemoteAddr) would break the deployment we
// actually run.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Fly-Client-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return trimSpace(xff[:i])
			}
		}
		return trimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
