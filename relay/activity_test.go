package relay

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
)

func assertActivity(t *testing.T, state *State, active int, revision int64) Snapshot {
	t.Helper()
	snap := state.Snapshot()
	if snap.ActiveConnections != active || snap.ActivityRevision != revision {
		t.Fatalf("activity = (%d, %d), want (%d, %d)",
			snap.ActiveConnections, snap.ActivityRevision, active, revision)
	}
	return snap
}

func TestPendingAndLiveStreamsShareOneActivityReservation(t *testing.T) {
	tun, _ := tunnelPair(t)
	state := NewState()
	tun.state = state
	s := tun.reserveStream(7)
	if s == nil {
		t.Fatal("reservation failed")
	}
	if snap := assertActivity(t, state, 1, 1); snap.Connections != 0 {
		t.Fatal("a pending stream changed cumulative reached destinations")
	}
	if tun.reserveStream(7) != nil {
		t.Fatal("duplicate stream accepted")
	}
	assertActivity(t, state, 1, 1)
	local, remote := net.Pipe()
	defer remote.Close()
	s.conn = local
	state.NoteDestination("example.com", 443)
	if snap := assertActivity(t, state, 1, 1); snap.Connections != 1 {
		t.Fatal("reached destination did not retain the cumulative counter")
	}
	if !tun.dropStream(7, s) {
		t.Fatal("live stream was not removed")
	}
	if snap := assertActivity(t, state, 0, 2); snap.Connections != 1 {
		t.Fatal("dropping a stream reset cumulative reached destinations")
	}
	// A stream can start and finish between two Android polls. The count is
	// zero again, but the revision must prove that the interval was not idle.
	if tun.dropStream(7, s) {
		t.Fatal("duplicate drop removed a stream")
	}
	assertActivity(t, state, 0, 2)
	newStream := tun.reserveStream(7)
	if newStream == nil {
		t.Fatal("reused ID was not reserved")
	}
	if tun.dropStream(7, s) {
		t.Fatal("stale worker removed the reused stream ID")
	}
	assertActivity(t, state, 1, 3)
	tun.Close()
	assertActivity(t, state, 0, 4)
	tun.Close()
	if tun.dropStream(7, newStream) || tun.reserveStream(8) != nil {
		t.Fatal("closed tunnel accepted more stream work")
	}
	assertActivity(t, state, 0, 4)
}

func TestStreamLimitRefusalDoesNotStartActivity(t *testing.T) {
	tun, _ := tunnelPair(t)
	state := NewState()
	tun.state = state
	for id := uint32(0); id < maxStreams; id++ {
		if tun.reserveStream(id) == nil {
			t.Fatal("premature stream limit")
		}
	}
	assertActivity(t, state, maxStreams, maxStreams)
	if tun.reserveStream(maxStreams) != nil {
		t.Fatal("stream limit was exceeded")
	}
	assertActivity(t, state, maxStreams, maxStreams)
	tun.Close()
	assertActivity(t, state, 0, 2*maxStreams)
}

func TestRefusedDestinationEndsPendingActivity(t *testing.T) {
	tun, _ := tunnelPair(t)
	state := NewState()
	tun.state = state
	tun.dispatch(frame{op: opOpen, streamID: 1, payload: []byte(`{"host":"127.0.0.1","port":443}`)})
	tun.workers.Wait()
	snap := assertActivity(t, state, 0, 2)
	if snap.Connections != 0 || len(snap.Recent) != 1 || snap.Recent[0].Allowed {
		t.Fatal("policy refusal did not remain separate from reached destinations")
	}
	if len(tun.openSlots) != 0 {
		t.Fatal("refused stream retained its worker slot")
	}
}

func TestConcurrentCloseDropTrafficAndSnapshotsBalanceActivity(t *testing.T) {
	tun, _ := tunnelPair(t)
	state := NewState()
	tun.state = state
	const count = 16
	streams := make([]*stream, count)
	for i := range streams {
		streams[i] = tun.reserveStream(uint32(i))
		if streams[i] == nil {
			t.Fatal("reservation failed")
		}
	}
	assertActivity(t, state, count, count)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, s := range streams {
		workers.Add(1)
		go func(s *stream) {
			defer workers.Done()
			<-start
			tun.dropStream(s.id, s)
			tun.dropStream(s.id, s)
		}(s)
	}
	workers.Add(3)
	go func() {
		defer workers.Done()
		<-start
		tun.Close()
		tun.Close()
	}()
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < 100; i++ {
			tun.addBytes(1)
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		var revision int64
		for i := 0; i < 1000; i++ {
			snap := state.Snapshot()
			if snap.ActiveConnections < 0 || snap.ActivityRevision < revision {
				t.Error("concurrent status reported negative activity or a decreasing revision")
				return
			}
			revision = snap.ActivityRevision
		}
	}()
	close(start)
	workers.Wait()
	snap := assertActivity(t, state, 0, 2*count+100)
	if snap.Bytes != 100 || tun.Bytes() != 100 || snap.Connections != 0 {
		t.Fatal("traffic or cumulative destinations were corrupted during close")
	}
}

func TestOnlyPositiveTrafficAdvancesActivityAndJSONRetainsCounters(t *testing.T) {
	tun, _ := tunnelPair(t)
	state := NewState()
	tun.state = state
	tun.addBytes(0)
	tun.addBytes(-1)
	state.AddBytes(0)
	state.AddBytes(-1)
	assertActivity(t, state, 0, 0)
	tun.addBytes(5)
	tun.addBytes(3)
	snap := assertActivity(t, state, 0, 2)
	if snap.Bytes != 8 || tun.Bytes() != 8 {
		t.Fatal("nonpositive traffic changed cumulative bytes")
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"active_connections": "0", "activity_revision": "2", "connections": "0", "bytes": "8",
	} {
		if string(fields[key]) != expected {
			t.Errorf("JSON field %s = %s, want %s", key, fields[key], expected)
		}
	}
}
