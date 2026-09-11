package main

import (
	"context"
	"sync"
)

// relayClient owns settings and the running session. The UI and tray use the
// same operations, so saving a setting and stopping its old session stay together.
type relayClient struct {
	mu      sync.Mutex
	cfg     Config
	state   *State
	changed chan struct{}
	cancel  context.CancelFunc
	tunnel  *tunnel
}

func newRelayClient(cfg Config, state *State) *relayClient {
	return &relayClient{cfg: cfg, state: state, changed: make(chan struct{})}
}

func (c *relayClient) config() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

func (c *relayClient) setToken(token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	saved := loadStored()
	saved.Token = token
	if err := saveStored(saved); err != nil {
		return err
	}
	c.cfg.Token = token
	c.restartLocked()
	return nil
}

func (c *relayClient) togglePause() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	paused := !c.state.Paused()
	saved := loadStored()
	saved.Paused = paused
	if err := saveStored(saved); err != nil {
		return !paused, err
	}
	c.state.SetPaused(paused)
	c.restartLocked()
	return paused, nil
}

// Caller holds mu. Cancel pending DNS/dials and close the active sockets before
// reporting a settings change as successful.
func (c *relayClient) restartLocked() {
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

func (c *relayClient) session(parent context.Context) (context.Context, Config, bool, <-chan struct{}) {
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

func (c *relayClient) attach(ctx context.Context, t *tunnel) bool {
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

func (c *relayClient) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restartLocked()
}
