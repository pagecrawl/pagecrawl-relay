package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// A tiny settings page served on loopback, so someone who has never opened a
// terminal can paste a token, see that it is working, and turn it off again.
//
// Why a local web page rather than a native window: it needs no GUI toolkit, so
// the binary stays pure Go and cross-compiles to every platform from one machine.
// The menu-bar build just opens this page.

// uiServer owns the loopback listener and the key that protects it.
type uiServer struct {
	state   *State
	cfg     *Config
	key     string
	addr    string
	onToken func(string)
	onPause func(bool)
}

// A page in someone's browser can POST to 127.0.0.1 just as easily as this app can:
// that is ordinary CSRF, and the target here is the enrolment token. Two defences,
// because either alone has gaps:
//
//   - a random key minted per run, handed to the browser in the URL we open and
//     required on every mutating request, which a third-party page cannot guess; and
//   - an Origin check, which blocks a page that somehow learned the key from
//     replaying it cross-origin.
func (s *uiServer) authorised(r *http.Request) bool {
	// Exact match, never a prefix. A prefix test on a host token accepts anything
	// that merely STARTS with it, so "http://localhost" would also match
	// "http://localhost.evil.com", and "http://127.0.0.1:54321" would match
	// "http://127.0.0.1:54321.evil.com" - both domains an attacker can register.
	if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
		return false
	}

	supplied := r.Header.Get("X-Relay-Key")
	if supplied == "" {
		supplied = r.URL.Query().Get("k")
	}

	return subtle.ConstantTimeCompare([]byte(supplied), []byte(s.key)) == 1
}

// originAllowed accepts only the two origins this page is ever served from: the
// listen address itself, and the same port under the "localhost" name a browser may
// substitute for 127.0.0.1.
func (s *uiServer) originAllowed(origin string) bool {
	_, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		// Cannot determine the port, so cannot authorise anything on host grounds.
		return false
	}

	for _, allowed := range []string{
		"http://" + s.addr,
		"http://localhost:" + port,
		"http://[::1]:" + port,
	} {
		if origin == allowed {
			return true
		}
	}

	return false
}

func (s *uiServer) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// No external requests: the page must work on a machine with no internet,
		// which is exactly the machine someone is trying to diagnose.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
		fmt.Fprint(w, settingsPage)
	})

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorised(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		snap := s.state.Snapshot()
		snap.ExitIP = firstNonEmpty(snap.ExitIP, "")
		writeJSON(w, map[string]any{
			"state":      snap,
			"configured": s.cfg.Token != "",
			"gateway":    s.cfg.GatewayURL,
			"version":    Version,
			"platform":   platformName(),
		})
	})

	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !s.authorised(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		token := strings.TrimSpace(body.Token)
		if len(token) < 32 {
			writeJSON(w, map[string]any{"ok": false, "error": "That does not look like a relay token. Copy the whole value shown when you added the machine."})
			return
		}

		saved := loadStored()
		saved.Token = token
		if err := saveStored(saved); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "Could not save: " + err.Error()})
			return
		}

		s.cfg.Token = token
		if s.onToken != nil {
			s.onToken(token)
		}

		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/pause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !s.authorised(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		paused := !s.state.Paused()
		s.state.SetPaused(paused)

		saved := loadStored()
		saved.Paused = paused
		_ = saveStored(saved)

		if s.onPause != nil {
			s.onPause(paused)
		}

		writeJSON(w, map[string]any{"ok": true, "paused": paused})
	})

	// Forgetting the token is the "get me out of this" control. It must exist on the
	// page, because someone who wants to stop relaying should not have to find a
	// config file to do it.
	mux.HandleFunc("/api/forget", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !s.authorised(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		saved := loadStored()
		saved.Token = ""
		if err := saveStored(saved); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "Could not clear it: " + err.Error()})
			return
		}

		s.cfg.Token = ""

		// Nudge the connection loop so the tunnel drops now rather than at the next
		// reconnect: "disconnect" should mean disconnected.
		if s.onToken != nil {
			s.onToken("")
		}

		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/check", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorised(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"checks": runDoctor(*s.cfg, s.cfg.Token != "")})
	})

	return mux
}

// startUI binds loopback and returns the URL to open. Loopback only: this page can
// set the enrolment token, so it must never be reachable from the network.
func startUI(state *State, cfg *Config, onToken func(string), onPause func(bool)) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}

	srv := &uiServer{
		state:   state,
		cfg:     cfg,
		key:     randomKey(),
		addr:    ln.Addr().String(),
		onToken: onToken,
		onPause: onPause,
	}

	server := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() { _ = server.Serve(ln) }()

	return fmt.Sprintf("http://%s/?k=%s", srv.addr, srv.key), nil
}

// openBrowser is best effort: if it fails the caller still prints the URL, which is
// all someone needs to open it themselves.
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}

	_ = exec.Command(cmd, append(args, url)...).Start()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
