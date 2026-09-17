package relay

import (
	"context"
	"errors"
	"testing"
)

// memoryStore keeps settings in memory, for tests that are about the tunnel rather than
// about where settings are saved.
type memoryStore struct{}

func (memoryStore) SaveToken(string) error { return nil }
func (memoryStore) SavePaused(bool) error  { return nil }

// failingStore refuses every write.
type failingStore struct{}

var errStoreFull = errors.New("store refused the write")

func (failingStore) SaveToken(string) error { return errStoreFull }
func (failingStore) SavePaused(bool) error  { return errStoreFull }

// A change the store could not keep must not be reported as made, or the relay would
// carry on with a setting that is gone after a restart. This holds for every front end,
// which is why it is tested here against the interface rather than against a file.
func TestSettingChangeIsNotAppliedWhenTheStoreRefusesIt(t *testing.T) {
	client := NewClient(Config{Token: "original"}, NewState(), failingStore{})
	defer client.Stop()
	session, _, _, _ := client.session(context.Background())

	if err := client.SetToken("new"); !errors.Is(err, errStoreFull) {
		t.Fatalf("SetToken error = %v, want the store's error", err)
	}
	if got := client.Config().Token; got != "original" {
		t.Fatalf("token after a refused save = %q, want it unchanged", got)
	}

	if paused, err := client.TogglePause(); !errors.Is(err, errStoreFull) || paused {
		t.Fatalf("TogglePause = (%v, %v), want (false, the store's error)", paused, err)
	}
	if client.State().Paused() {
		t.Fatal("relay paused although the store refused to keep it")
	}
	// A setting that was not saved is not a setting change, so the tunnel carrying
	// checks right now must not be torn down for it.
	if session.Err() != nil {
		t.Fatal("a refused save restarted the running session")
	}
}
