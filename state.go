package main

import (
	"fmt"
	"sync"
	"time"
)

// Live state, shared between the connection loop and whatever is displaying it
// (the menu bar, the local settings page, the terminal). One small mutex-guarded
// struct rather than channels: every reader wants a snapshot of everything, and
// readers come and go.
type State struct {
	mu sync.Mutex

	startedAt   time.Time
	connectedAt time.Time
	connected   bool
	paused      bool

	// Cumulative for the life of the process, so a reconnect does not reset the
	// number someone is watching.
	bytes    int64
	checks   int
	exitIP   string
	lastErr  string
	lastHost string

	// A short ring of recent destinations. This is the answer to "what is it
	// actually doing on my network", which is the question anyone running this on
	// their own hardware is entitled to ask. Bounded, and never persisted: it is a
	// window on the present, not a log to be mined later.
	recent []Event
}

// Event is one destination this machine was asked to reach.
type Event struct {
	At      string `json:"at"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// recentLimit is deliberately small: a page load touches dozens of hosts, so a
// longer ring would be noise rather than insight, and it all lives in memory.
const recentLimit = 40

func NewState() *State {
	return &State{startedAt: time.Now()}
}

func (s *State) MarkConnected(exitIP string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = true
	s.connectedAt = time.Now()
	s.lastErr = ""
	if exitIP != "" {
		s.exitIP = exitIP
	}
}

func (s *State) MarkDisconnected(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	if err != nil {
		s.lastErr = err.Error()
	}
}

func (s *State) AddBytes(n int64) {
	s.mu.Lock()
	s.bytes += n
	s.mu.Unlock()
}

// NoteDestination records a destination that was reached.
func (s *State) NoteDestination(host string, port int) {
	s.mu.Lock()
	s.lastHost = host
	s.checks++
	s.push(Event{At: time.Now().Format("15:04:05"), Host: host, Port: port, Allowed: true})
	s.mu.Unlock()
}

// NoteRefused records a destination this machine declined to reach, with why.
//
// Refusals matter more than successes here: they are the guard doing its job, and
// seeing them is what makes the promise checkable rather than something to believe.
func (s *State) NoteRefused(host string, port int, reason string) {
	s.mu.Lock()
	s.push(Event{At: time.Now().Format("15:04:05"), Host: host, Port: port, Allowed: false, Reason: reason})
	s.mu.Unlock()
}

// push appends to the ring. Caller holds the lock.
func (s *State) push(e Event) {
	s.recent = append(s.recent, e)
	if len(s.recent) > recentLimit {
		s.recent = s.recent[len(s.recent)-recentLimit:]
	}
}

func (s *State) SetPaused(p bool) {
	s.mu.Lock()
	s.paused = p
	s.mu.Unlock()
}

func (s *State) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.paused
}

// Snapshot is what every display reads. Returned by value so a caller can render
// it without holding the lock.
type Snapshot struct {
	Connected    bool    `json:"connected"`
	Paused       bool    `json:"paused"`
	Uptime       string  `json:"uptime"`
	ConnectedFor string  `json:"connected_for"`
	Bytes        int64   `json:"bytes"`
	Traffic      string  `json:"traffic"`
	Connections  int     `json:"connections"`
	ExitIP       string  `json:"exit_ip"`
	LastHost     string  `json:"last_host"`
	LastError    string  `json:"last_error"`
	Recent       []Event `json:"recent"`
}

func (s *State) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{
		Connected:   s.connected,
		Paused:      s.paused,
		Uptime:      humanDuration(time.Since(s.startedAt)),
		Bytes:       s.bytes,
		Traffic:     humanBytes(s.bytes),
		Connections: s.checks,
		ExitIP:      s.exitIP,
		LastHost:    s.lastHost,
		LastError:   s.lastErr,
	}

	// Copied, and newest first: the caller renders this without the lock, and the
	// interesting end of a ring is the recent end.
	snap.Recent = make([]Event, 0, len(s.recent))
	for i := len(s.recent) - 1; i >= 0; i-- {
		snap.Recent = append(snap.Recent, s.recent[i])
	}

	if s.connected && !s.connectedAt.IsZero() {
		snap.ConnectedFor = humanDuration(time.Since(s.connectedAt))
	}

	return snap
}

// humanDuration renders a span the way someone glancing at a menu bar reads it:
// the largest two units, never seconds once it has been up for an hour.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}

	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}
