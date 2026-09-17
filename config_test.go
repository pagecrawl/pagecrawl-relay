package main

import (
	"os"
	"runtime"
	"testing"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

func TestConfigWriteReplacesBroadPermissions(t *testing.T) {
	isolatedConfig(t)
	if err := saveStored(stored{Token: "first"}); err != nil {
		t.Fatal(err)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveStored(stored{Token: "second", Paused: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadStored(); got.Token != "second" || !got.Paused {
		t.Fatal(got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions:", info.Mode())
	}
}

// The relay only applies a setting the store says it kept (see relay/store_test.go), so
// the file store has to report a write it could not make rather than swallow it.
func TestFailedSettingWriteDoesNotClaimSuccess(t *testing.T) {
	isolatedConfig(t)
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	client := relay.NewClient(relay.Config{Token: "original"}, relay.NewState(), fileStore{})
	defer client.Stop()
	if err := client.SetToken("new"); err == nil {
		t.Fatal("expected save failure")
	}
	if _, err := client.TogglePause(); err == nil {
		t.Fatal("expected pause save failure")
	}
	if client.Config().Token != "original" || client.State().Paused() {
		t.Fatal("failed persistence changed the running settings")
	}
}

// isolatedConfig points the config directory at a temporary one, so a test never reads
// or overwrites the settings of a relay installed on this machine.
func isolatedConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func TestConfiguredTokenTrimsPastedWhitespace(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("PAGECRAWL_RELAY_TOKEN", "  environment-token\n")
	cfg, present := resolveConfig("", " \n", false)
	if !present || cfg.Token != "environment-token" {
		t.Fatal("token normalization changed precedence")
	}
}

// The gateway is told which version and platform connected. The version is stamped at
// build time with -X main.Version (release workflow, Dockerfile, Home Assistant add-on),
// which only package main can see, so it has to be carried into the relay's config here
// or every release would report itself as "dev".
func TestConfigCarriesTheStampedVersionAndPlatform(t *testing.T) {
	isolatedConfig(t)
	previous := Version
	Version = "9.8.7"
	t.Cleanup(func() { Version = previous })

	cfg, _ := resolveConfig("", "token", false)

	if cfg.Version != "9.8.7" {
		t.Fatalf("version = %q, want the stamped one", cfg.Version)
	}
	if cfg.Platform != platformName() {
		t.Fatalf("platform = %q, want %q", cfg.Platform, platformName())
	}
}
