// Package mobile is the PageCrawl Relay as an Android library, built with gomobile bind.
//
// It is a thin wrapper and deliberately so. Everything that decides what this phone will
// connect to (the destination guard), how it talks to the gateway and how it reconnects
// is package relay, the same code the desktop program runs. This file only turns that
// into calls a Kotlin foreground service can make: start, stop, change the key, and read
// the status back.
//
// Types crossing the gomobile bridge are kept to strings, booleans and errors. Status and
// self-check results travel as JSON, so a new field is a JSON key rather than a change to
// the generated Java API.
package mobile

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

// Relay is one relay running inside the Android app.
type Relay struct {
	mu      sync.Mutex
	client  *relay.Client
	cancel  context.CancelFunc
	stopped chan struct{}
}

// NewRelay prepares a relay for the given key. platform is reported to the gateway (for
// example "android/arm64"), version is the app's version. Nothing connects until Start.
func NewRelay(token, platform, version string) *Relay {
	cfg := relay.DefaultConfig()
	cfg.Token = strings.TrimSpace(token)
	cfg.Platform = platform
	cfg.Version = version

	// The app keeps the key in its own encrypted storage and decides when the relay runs
	// (enabled by the person, and on a network they allowed), so the relay's own pause
	// flag and saved settings are not used here.
	return &Relay{client: relay.NewClient(cfg, relay.NewState(), ignoredStore{})}
}

// Start connects and keeps the relay connected until Stop. Calling it while running
// does nothing.
func (r *Relay) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	r.cancel = cancel
	r.stopped = stopped

	go func() {
		defer close(stopped)
		relay.Supervise(ctx, r.client)
	}()
}

// Stop disconnects and returns once the relay has fully stopped, so a Start straight
// after it can never run two tunnels at once. Calling it while stopped does nothing.
func (r *Relay) Stop() {
	r.mu.Lock()
	cancel, stopped := r.cancel, r.stopped
	r.cancel, r.stopped = nil, nil
	r.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	r.client.Stop()
	<-stopped
}

// Running reports whether Start has been called without a Stop since.
func (r *Relay) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.cancel != nil
}

// SetToken replaces the key. A running relay reconnects with the new one.
func (r *Relay) SetToken(token string) error {
	return r.client.SetToken(strings.TrimSpace(token))
}

// StatusJSON is the live status as JSON: connected, traffic, connections carried, the
// last error and recent destinations (see relay.Snapshot for the fields).
func (r *Relay) StatusJSON() string {
	return mustJSON(r.client.State().Snapshot())
}

// CheckJSON runs the self-checks the desktop program runs for -check and returns them
// as a JSON list of {name, ok, detail, fix}. It makes network requests and can take up
// to about 40 seconds, so the app calls it off the main thread.
func (r *Relay) CheckJSON() string {
	return mustJSON(relay.RunDoctor(r.client.Config()))
}

// ignoredStore accepts every setting without keeping it: the app persists the key itself.
type ignoredStore struct{}

func (ignoredStore) SaveToken(string) error { return nil }
func (ignoredStore) SavePaused(bool) error  { return nil }

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}

	return string(data)
}
