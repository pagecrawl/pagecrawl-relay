package relay

import (
	"context"
	"sync"
)

// Client owns settings and the running session. Every front end (the desktop settings
// page and tray, the Android service) uses the same operations, so saving a setting and
// stopping its old session stay together.
type Client struct {
	mu      sync.Mutex
	cfg     Config
	state   *State
	store   Store
	changed chan struct{}
	cancel  context.CancelFunc
	tunnel  *tunnel
}

// NewClient makes a client that persists setting changes through store.
func NewClient(cfg Config, state *State, store Store) *Client {
	return &Client{cfg: cfg, state: state, store: store, changed: make(chan struct{})}
}

// State is the live status the front end shows: connected, bytes, recent destinations.
func (c *Client) State() *State {
	return c.state
}

func (c *Client) Config() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

func (c *Client) SetToken(token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.store.SaveToken(token); err != nil {
		return err
	}
	c.cfg.Token = token
	c.restartLocked()
	return nil
}

func (c *Client) TogglePause() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	paused := !c.state.Paused()
	if err := c.store.SavePaused(paused); err != nil {
		return !paused, err
	}
	c.state.SetPaused(paused)
	c.restartLocked()
	return paused, nil
}

// Caller holds mu. Cancel pending DNS/dials and close the active sockets before
// reporting a settings change as successful.
func (c *Client) restartLocked() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.tunnel != nil {
		c.tunnel.Close()
		c.tunnel = nil
	}
	c.state.MarkDisconnected(nil)
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *Client) session(parent context.Context) (context.Context, Config, bool, <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.tunnel = nil
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	return ctx, c.cfg, c.state.Paused(), c.changed
}

func (c *Client) attach(ctx context.Context, t *tunnel) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil {
		t.Close()
		return false
	}
	c.tunnel = t
	c.state.MarkConnected("")
	return true
}

// Stop closes the running session. Supervise keeps waiting for a setting change until
// its context is done, so a caller that is finished cancels that context as well.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restartLocked()
}
