package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The loopback settings page keeps the default binary independent of GUI toolkits.

// uiServer owns the loopback listener and the key that protects it.
type uiServer struct {
	client  *relayClient
	key     string
	addr    string
	checkMu sync.Mutex
}

// Require the per-run key and reject foreign Origins. Loopback alone does not
// prevent another page in the operator's browser from calling this API.
func (s *uiServer) authorised(r *http.Request) bool {
	// Compare whole origins, never prefixes such as localhost.evil.com.
	if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
		return false
	}

	supplied := r.Header.Get("X-Relay-Key")
	if supplied == "" {
		supplied = r.URL.Query().Get("k")
	}

	return subtle.ConstantTimeCompare([]byte(supplied), []byte(s.key)) == 1
}

// Origins must match exactly, including their port and scheme.
func (s *uiServer) originAllowed(origin string) bool {
	_, port, err := net.SplitHostPort(s.addr)
	return err == nil && (origin == "http://"+s.addr ||
		origin == "http://localhost:"+port || origin == "http://[::1]:"+port)
}

func (s *uiServer) api(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != method || !s.authorised(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		handler(w, r)
	}
}

func (s *uiServer) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// The page needs same-origin fetches, but no external assets or requests.
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'none'; connect-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'",
		)
		fmt.Fprint(w, settingsPage)
	})

	mux.HandleFunc("/api/state", s.api(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		cfg := s.client.config()
		writeJSON(w, map[string]any{
			"state":      s.client.state.Snapshot(),
			"configured": cfg.Token != "",
			"gateway":    cfg.GatewayURL,
			"version":    Version,
			"platform":   platformName(),
		})
	}))

	mux.HandleFunc("/api/token", s.api(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
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
		s.saveToken(w, token)
	}))

	mux.HandleFunc("/api/forget", s.api(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		s.saveToken(w, "")
	}))

	mux.HandleFunc("/api/pause", s.api(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		paused, err := s.client.togglePause()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "Could not save: " + err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "paused": paused})
	}))

	mux.HandleFunc("/api/check", s.api(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		if !s.checkMu.TryLock() {
			http.Error(w, "a self-check is already running", http.StatusTooManyRequests)
			return
		}
		defer s.checkMu.Unlock()
		writeJSON(w, map[string]any{"checks": runDoctorContext(r.Context(), s.client.config())})
	}))

	return mux
}

func (s *uiServer) saveToken(w http.ResponseWriter, token string) {
	if err := s.client.setToken(token); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "Could not save: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// A stable port makes reopened pages discoverable. The key still changes on
// each run, and an occupied port falls back to a free loopback port.
const preferredUIPort = 28472

// startUI binds loopback and returns the URL to open. Loopback only: this page can
// set the enrolment token, so it must never be reachable from the network.
func startUI(ctx context.Context, client *relayClient) (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredUIPort))
	if err != nil {
		// Taken, most likely by a second copy of this program. Any free port still
		// works; the URL is printed and opened, so nothing depends on guessing it.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}

	if err != nil {
		return "", err
	}

	key, err := randomKey()
	if err != nil {
		_ = ln.Close()
		return "", fmt.Errorf("generate settings key: %w", err)
	}
	srv := &uiServer{client: client, key: key, addr: ln.Addr().String()}

	server := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	go func() {
		stop := context.AfterFunc(ctx, func() { _ = server.Close() })
		defer stop()
		_ = server.Serve(ln)
	}()

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
