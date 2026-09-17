package relay

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/gorilla/websocket"
)

// Gateway-specific websocket close codes for a missing or unauthorized token.
// These use the private 4000-4999 range, so no library constant covers them.
const (
	closeMissingToken = 4001
	closeUnauthorized = 4003
)

// Supervise uses one retry path for failed dials and disconnected sessions. It
// returns when ctx is done.
func Supervise(ctx context.Context, client *Client) {
	backoff := time.Second
	for ctx.Err() == nil {
		session, cfg, paused, changed := client.session(ctx)
		if cfg.Token == "" || paused {
			select {
			case <-ctx.Done():
			case <-changed:
			}
			continue
		}

		conn, err := dialContext(session, cfg)
		if err == nil && client.attach(session, conn) {
			conn.state = client.state
			startedAt := time.Now()
			log.Printf("Connected to %s as %s.", cfg.GatewayURL, cfg.Platform)
			err = conn.Run()
			if time.Since(startedAt) > healthySession {
				backoff = time.Second
			}
		}
		client.state.MarkDisconnected(err)
		if IsRejected(err) {
			backoff = cfg.MaxBackoff
			log.Print("The gateway rejected this token. Enrol this machine again in Settings -> Relays.")
		}
		if session.Err() != nil {
			backoff = time.Second
			continue
		}
		log.Printf("Not connected (%v). Retrying in %s.", err, backoff.Round(time.Second))
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
		case <-changed:
			backoff = time.Second
		case <-timer.C:
			backoff = growBackoff(backoff, cfg.MaxBackoff)
		}
		timer.Stop()
	}
}

// How long a connection has to last before it counts as healthy. Shorter than any
// real working session and longer than a handshake-then-reject, which is the whole
// distinction being drawn.
const healthySession = 60 * time.Second

// grow the wait exponentially, with jitter so a gateway restart does not bring
// every relay in the fleet back in the same instant.
func growBackoff(backoff, max time.Duration) time.Duration {
	backoff = time.Duration(float64(backoff) * 1.7)
	if backoff > max {
		backoff = max
	}

	return backoff + jitter()
}

// IsRejected reports whether the gateway turned this relay away for a reason that
// will still be true in a second: a missing, unknown or revoked token. Anything
// else (a dropped link, a gateway restart) is worth retrying promptly.
func IsRejected(err error) bool {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code == closeMissingToken || closeErr.Code == closeUnauthorized
	}

	return false
}

func jitter() time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(time.Second)))
	if err != nil {
		return 0
	}

	return time.Duration(n.Int64())
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
