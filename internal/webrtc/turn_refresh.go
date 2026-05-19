package webrtc

import (
	"strconv"
	"strings"
	"time"

	pion "github.com/pion/webrtc/v4"
)

const (
	defaultTURNRefreshMargin = 60 * time.Second
	minTURNRefreshMargin     = 15 * time.Second
)

// TURNRESTExpiryUnix parses the expiry unix timestamp from a coturn REST username.
func TURNRESTExpiryUnix(username string) (time.Time, bool) {
	i := strings.Index(username, ":")
	if i <= 0 {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(username[:i], 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

func turnRefreshSleep(expiry time.Time, ttl time.Duration) time.Duration {
	if ttl <= 0 {
		ttl = defaultICEAuthTTL
	}
	margin := ttl / 4
	if margin < minTURNRefreshMargin {
		margin = minTURNRefreshMargin
	}
	if margin > defaultTURNRefreshMargin {
		margin = defaultTURNRefreshMargin
	}
	wake := expiry.Add(-margin)
	d := time.Until(wake)
	if d < time.Second {
		return time.Second
	}
	return d
}

func (b *Bridge) startTURNRefreshLoop() {
	if b == nil || b.pc == nil || strings.TrimSpace(b.opts.ICEAuthSecret) == "" {
		return
	}
	servers, err := BuildICEServers(b.opts)
	if err != nil || len(servers) == 0 {
		return
	}
	var expiry time.Time
	for _, s := range servers {
		if t, ok := TURNRESTExpiryUnix(s.Username); ok {
			expiry = t
			break
		}
	}
	if expiry.IsZero() {
		return
	}
	b.stateMu.Lock()
	b.turnCredExpires = expiry.Unix()
	b.stateMu.Unlock()

	go b.turnRefreshLoop(expiry, b.opts.iceAuthTTL())
}

func (b *Bridge) turnRefreshLoop(firstExpiry time.Time, ttl time.Duration) {
	expiry := firstExpiry
	for {
		sleep := turnRefreshSleep(expiry, ttl)
		select {
		case <-b.closed:
			return
		case <-time.After(sleep):
		}
		if err := b.refreshTURNCredentials(); err != nil {
			if b.opts.Logger != nil {
				b.opts.Logger.Warn("webrtc: turn credential refresh failed", "err", err)
			}
			select {
			case <-b.closed:
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}
		b.stateMu.RLock()
		next := b.turnCredExpires
		b.stateMu.RUnlock()
		if next <= 0 {
			return
		}
		expiry = time.Unix(next, 0)
	}
}

func (b *Bridge) refreshTURNCredentials() error {
	if b == nil || b.pc == nil {
		return nil
	}
	servers, err := BuildICEServers(b.opts)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return nil
	}
	if err := b.pc.SetConfiguration(pion.Configuration{ICEServers: servers}); err != nil {
		return err
	}
	var expiry int64
	for _, s := range servers {
		if t, ok := TURNRESTExpiryUnix(s.Username); ok {
			expiry = t.Unix()
			break
		}
	}
	b.stateMu.Lock()
	b.turnRefreshCount++
	if expiry > 0 {
		b.turnCredExpires = expiry
	}
	b.stateMu.Unlock()
	if b.opts.Logger != nil {
		b.opts.Logger.Debug("webrtc: refreshed TURN REST credentials", "expires_unix", expiry)
	}
	return nil
}
