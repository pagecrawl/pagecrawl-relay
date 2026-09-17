package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

// Settings persist desktop enrolment. Flags override environment variables,
// which override this file.
type stored struct {
	Token      string `json:"token"`
	GatewayURL string `json:"gateway_url,omitempty"`
	Paused     bool   `json:"paused,omitempty"`
}

// configPath uses the platform config directory, or a relative directory when
// no user config directory exists (for example, in a container).
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

// Where the running instance leaves the settings-page URL, key and all.
//
// Written 0600 beside the config so -open can reopen the authenticated page.
// Removed on normal exit; a new run replaces its key.
func uiURLPath() (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}

	return filepath.Join(filepath.Dir(path), "settings-url"), nil
}

// rememberUIURL records where the settings page is, for `-open` to find later.
func rememberUIURL(url string) {
	path, err := uiURLPath()
	if err != nil {
		return
	}

	// Best effort: not being able to write this costs the convenience of -open, and
	// nothing else. It must never stop the relay starting.
	_ = writePrivateFile(path, []byte(url))
}

// forgetUIURL drops the record on the way out, so -open never points at a dead port.
func forgetUIURL() {
	if path, err := uiURLPath(); err == nil {
		_ = os.Remove(path)
	}
}

// storedUIURL reads back what a running instance left.
func storedUIURL() string {
	path, err := uiURLPath()
	if err != nil {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
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

	return writePrivateFile(path, data)
}

// A new 0600 file also repairs overly broad permissions on an existing file.
// Rename prevents readers from seeing a partially written credential.
func writePrivateFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".relay-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// resolveConfig folds flags, environment and the config file into one Config, and
// reports whether a token was found anywhere.
func resolveConfig(flagGateway, flagToken string, verbose bool) (relay.Config, bool) {
	saved := loadStored()

	token := firstNonEmpty(strings.TrimSpace(flagToken), strings.TrimSpace(os.Getenv("PAGECRAWL_RELAY_TOKEN")), strings.TrimSpace(saved.Token))

	gateway := firstNonEmpty(
		flagGateway,
		os.Getenv("PAGECRAWL_RELAY_GATEWAY"),
		saved.GatewayURL,
		relay.DefaultGateway,
	)

	cfg := relay.DefaultConfig()
	cfg.GatewayURL = gateway
	cfg.Token = token
	cfg.Verbose = verbose
	cfg.Platform = platformName()
	cfg.Version = Version

	return cfg, token != ""
}

// fileStore keeps setting changes in the desktop config file. Each write reads the file
// first, so a change to one setting never drops another.
type fileStore struct{}

func (fileStore) SaveToken(token string) error {
	saved := loadStored()
	saved.Token = token
	return saveStored(saved)
}

func (fileStore) SavePaused(paused bool) error {
	saved := loadStored()
	saved.Paused = paused
	return saveStored(saved)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
