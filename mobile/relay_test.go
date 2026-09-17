package mobile

import (
	"encoding/json"
	"testing"
	"time"
)

// An empty key keeps these tests off the network: with no key the relay waits for one
// rather than dialling the gateway.
func newIdleRelay() *Relay {
	return NewRelay("", "android/arm64", "test")
}

func stopsWithin(t *testing.T, r *Relay, limit time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		r.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatal("Stop did not return")
	}
}

func TestStartThenStopLeavesNothingRunning(t *testing.T) {
	r := newIdleRelay()

	r.Start()
	if !r.Running() {
		t.Fatal("not running after Start")
	}

	stopsWithin(t, r, 5*time.Second)
	if r.Running() {
		t.Fatal("still running after Stop")
	}
}

// The service calls Start and Stop as the network comes and goes, often more than once
// for the same change, so neither may start a second relay or block on a stopped one.
func TestStartAndStopCanBeRepeated(t *testing.T) {
	r := newIdleRelay()

	r.Start()
	r.Start()
	stopsWithin(t, r, 5*time.Second)
	stopsWithin(t, r, time.Second)

	r.Start()
	if !r.Running() {
		t.Fatal("could not start again after stopping")
	}
	stopsWithin(t, r, 5*time.Second)
}

func TestStatusIsJSONTheAppCanRead(t *testing.T) {
	r := newIdleRelay()

	var status struct {
		Connected bool   `json:"connected"`
		Traffic   string `json:"traffic"`
	}
	if err := json.Unmarshal([]byte(r.StatusJSON()), &status); err != nil {
		t.Fatalf("status is not JSON: %v", err)
	}
	if status.Connected {
		t.Fatal("reported connected before starting")
	}
	if status.Traffic == "" {
		t.Fatal("traffic missing from the status")
	}
}

// A key pasted from a message or an email usually carries a newline.
func TestPastedKeyIsTrimmed(t *testing.T) {
	r := NewRelay("  key-from-the-web\n", "android/arm64", "test")
	if got := r.client.Config().Token; got != "key-from-the-web" {
		t.Fatalf("token = %q", got)
	}

	if err := r.SetToken("\tanother-key \n"); err != nil {
		t.Fatal(err)
	}
	if got := r.client.Config().Token; got != "another-key" {
		t.Fatalf("token after SetToken = %q", got)
	}
}

func TestPlatformAndVersionReachTheConfig(t *testing.T) {
	cfg := NewRelay("key", "android/arm64", "1.2.0").client.Config()
	if cfg.Platform != "android/arm64" || cfg.Version != "1.2.0" {
		t.Fatalf("config = %+v", cfg)
	}
}
