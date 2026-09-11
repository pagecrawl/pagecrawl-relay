// PageCrawl Relay client.
//
// Runs on a machine the customer owns and carries the network egress for THEIR OWN
// PageCrawl monitors. It dials out to the relay gateway, so it needs no port
// forwarding and no inbound firewall rule, and it only ever moves bytes for checks
// the gateway has already authorised against this machine's team.
//
// It is not a general-purpose proxy: it opens no listener for other traffic, and
// every destination is re-validated here (see guard.go) before a single byte moves.
//
// Three ways to run it, in the order most people need them:
//
//	pagecrawl-relay             desktop: opens a settings page, remembers the token
//	pagecrawl-relay -check      self-check, says in plain words what is wrong
//	pagecrawl-relay -headless   server: reads PAGECRAWL_RELAY_TOKEN, logs, no UI
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Gateway-specific websocket close codes for a missing or unauthorized token.
// These use the private 4000-4999 range, so no library constant covers them.
const (
	closeMissingToken = 4001
	closeUnauthorized = 4003
)

// Version is stamped at build time:
//
//	go build -ldflags "-X main.Version=1.2.3"
var Version = "dev"

const defaultGateway = "wss://relay.pagecrawl.io/tunnel"

type Config struct {
	GatewayURL  string
	Token       string
	Verbose     bool
	DialTimeout time.Duration
	IdleTimeout time.Duration
	MaxBackoff  time.Duration
}

func platformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func randomKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

func main() {
	var (
		gateway  = flag.String("gateway", "", "Relay gateway websocket URL")
		token    = flag.String("token", "", "Relay token from Settings -> Relays")
		verbose  = flag.Bool("verbose", false, "Log every destination, including ones that simply did not resolve")
		headless = flag.Bool("headless", false, "Never open a settings page: for servers, containers and services")
		check    = flag.Bool("check", false, "Run a self-check and exit")
		version  = flag.Bool("version", false, "Print the version and exit")
		open     = flag.Bool("open", false, "Open the settings page of the relay already running on this computer")
	)
	flag.Parse()

	if *version {
		fmt.Printf("pagecrawl-relay %s (%s)\n", Version, platformName())
		return
	}

	// Reopen the settings page of an instance already running here. The settings page
	// is protected by a key minted per run, so a bare visit to the port is refused;
	// this is the supported way back in without restarting the relay.
	if *open {
		url := storedUIURL()
		if url == "" {
			fmt.Println("No relay is running on this computer, or it was started with -headless.")
			fmt.Println("Start it with: pagecrawl-relay")
			os.Exit(1)
		}

		fmt.Printf("Settings: %s\n", url)
		openBrowser(url)

		return
	}

	cfg, hasToken := resolveConfig(*gateway, *token, *verbose)

	if *check {
		os.Exit(printDoctor(runDoctor(cfg)))
	}

	state := NewState()
	state.SetPaused(loadStored().Paused)

	// A server with no token has nothing to do and should say so loudly rather than
	// sit there looking healthy.
	if !hasToken && *headless {
		log.Fatal("No token. Enrol this machine at Settings -> Relays, then set PAGECRAWL_RELAY_TOKEN.")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := newRelayClient(cfg, state)
	defer client.stop()

	settingsURL := ""

	if !*headless {
		url, err := startUI(ctx, client)
		if err != nil {
			log.Printf("Could not open the settings page (%v). Carrying on without it.", err)
		} else {
			settingsURL = url
			// So the page can be reopened later. The key is minted per run and lives
			// only in memory, so without this, closing that tab locks the operator
			// out of their own relay until they restart it.
			rememberUIURL(url)
			defer forgetUIURL()

			fmt.Printf("PageCrawl Relay is running.\nSettings: %s\n", url)

			// Open the browser only when there is nothing to relay yet. Someone
			// already set up does not want a window every time they log in.
			if !hasToken && !hasTray() {
				openBrowser(url)
			}
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		supervise(ctx, client)
	}()

	if hasTray() && !*headless {
		runTray(ctx, client, settingsURL)
		cancel()
	} else {
		<-ctx.Done()
	}
	client.stop()
	<-done
	log.Println("Relay stopped. Monitors follow their configured relay fallback setting.")
}

// supervise uses one retry path for failed dials and disconnected sessions.
func supervise(ctx context.Context, client *relayClient) {
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
			log.Printf("Connected to %s as %s.", cfg.GatewayURL, platformName())
			err = conn.Run()
			if time.Since(startedAt) > healthySession {
				backoff = time.Second
			}
		}
		client.state.MarkDisconnected(err)
		if isRejected(err) {
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

// isRejected reports whether the gateway turned this relay away for a reason that
// will still be true in a second: a missing, unknown or revoked token. Anything
// else (a dropped link, a gateway restart) is worth retrying promptly.
func isRejected(err error) bool {
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
