package dashboard

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Login throttling: after loginFreeFailures consecutive failures for one
// client address, further attempts lock out for a window that
// doubles per extra failure up to loginMaxLockout. A success clears the
// entry. The key uses the socket peer address, never X-Forwarded-For: a
// forwarded header is client-controlled and would make the limiter optional.
const (
	loginFreeFailures = 5
	loginBaseLockout  = time.Minute
	loginMaxLockout   = 15 * time.Minute
	// loginEntryTTL drops entries idle this long; loginMaxEntries bounds the
	// table so a spray of addresses cannot grow memory without limit.
	loginEntryTTL   = time.Hour
	loginMaxEntries = 10000
)

type loginAttempts struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// loginLimiter tracks failed dashboard logins per client address and username.
// A nil limiter throttles nothing; New always builds one.
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]*loginAttempts
	now     func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{entries: map[string]*loginAttempts{}, now: time.Now}
}

func loginLimiterKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host
}

// retryAfter reports how long the key stays locked (0 = allowed).
func (l *loginLimiter) retryAfter(key string) time.Duration {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok {
		return 0
	}
	if wait := entry.lockedUntil.Sub(l.now()); wait > 0 {
		return wait
	}
	return 0
}

// recordFailure counts one failed attempt and extends the lockout.
func (l *loginLimiter) recordFailure(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.pruneLocked(now)
	entry, ok := l.entries[key]
	if !ok {
		entry = &loginAttempts{}
		l.entries[key] = entry
	}
	entry.failures++
	entry.lastSeen = now
	if entry.failures < loginFreeFailures {
		return
	}
	lockout := loginBaseLockout << (entry.failures - loginFreeFailures)
	if lockout <= 0 || lockout > loginMaxLockout {
		lockout = loginMaxLockout
	}
	entry.lockedUntil = now.Add(lockout)
}

// recordSuccess clears the key.
func (l *loginLimiter) recordSuccess(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < loginMaxEntries {
		return
	}
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > loginEntryTTL && !entry.lockedUntil.After(now) {
			delete(l.entries, key)
		}
	}
	if len(l.entries) < loginMaxEntries {
		return
	}
	// Still full: every entry is recent. Drop unlocked ones before locked
	// ones so active lockouts survive.
	for key, entry := range l.entries {
		if !entry.lockedUntil.After(now) {
			delete(l.entries, key)
		}
	}
}

// requestIsSecure reports whether the client reached the router over TLS,
// directly or through a TLS-terminating proxy. The header only adds the
// Secure cookie flag; trusting it cannot weaken anything.
func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
