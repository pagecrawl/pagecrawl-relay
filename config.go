package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Persisted settings, so someone who double-clicks the app once never has to think
// about a terminal or an environment variable again.
//
// Precedence, most specific first: command-line flag, environment variable, this
// file. A server operator setting PAGECRAWL_RELAY_TOKEN in a systemd unit or a
// docker-compose file therefore never has their config silently overridden by a
// stale file, and a desktop user never has to supply anything twice.
type stored struct {
	Token      string `json:"token"`
	GatewayURL string `json:"gateway_url,omitempty"`
	Paused     bool   `json:"paused,omitempty"`
}

// configPath returns the per-user config file, creating its directory.
//
// os.UserConfigDir is the platform-correct home for this: ~/Library/Application
// Support on macOS, %AppData% on Windows, ~/.config on Linux. A container with no
// home falls back to a path under the working directory so a bind-mounted volume
// still persists it.
func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = ".pagecrawl-relay"
	} else {
		base = filepath.Join(base, "pagecrawl-relay")
	}

	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}

	return filepath.Join(base, "config.json"), nil
}

func loadStored() stored {
	var s stored

	path, err := configPath()
	if err != nil {
		return s
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}

	_ = json.Unmarshal(data, &s)

	return s
}

// saveStored writes the config 0600: it holds the enrolment token, which is a
// bearer credential for this machine's relay.
func saveStored(s stored) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// resolveConfig folds flags, environment and the config file into one Config, and
// reports whether a token was found anywhere.
func resolveConfig(flagGateway, flagToken string, verbose bool) (Config, bool) {
	saved := loadStored()

	token := firstNonEmpty(flagToken, os.Getenv("PAGECRAWL_RELAY_TOKEN"), saved.Token)

	gateway := firstNonEmpty(
		flagGateway,
		os.Getenv("PAGECRAWL_RELAY_GATEWAY"),
		saved.GatewayURL,
		defaultGateway,
	)

	return Config{
		GatewayURL:  gateway,
		Token:       token,
		Verbose:     verbose,
		DialTimeout: 15 * time.Second,
		// Longer than any single page load, shorter than the worker's own per-attempt
		// cap, so a stalled stream is reclaimed before the check gives up.
		IdleTimeout: 120 * time.Second,
		MaxBackoff:  2 * time.Minute,
	}, token != ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
