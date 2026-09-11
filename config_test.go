package main

import (
	"context"
	"os"
	"runtime"
	"testing"
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

func TestFailedSettingWriteDoesNotClaimSuccess(t *testing.T) {
	isolatedConfig(t)
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	client := newRelayClient(Config{Token: "original"}, NewState())
	defer client.stop()
	ctx, _, _, _ := client.session(context.Background())
	if err := client.setToken("new"); err == nil {
		t.Fatal("expected save failure")
	}
	if _, err := client.togglePause(); err == nil {
		t.Fatal("expected pause save failure")
	}
	if client.config().Token != "original" || client.state.Paused() || ctx.Err() != nil {
		t.Fatal("failed persistence changed the running session")
	}
}

func TestConfiguredTokenTrimsPastedWhitespace(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("PAGECRAWL_RELAY_TOKEN", "  environment-token\n")
	cfg, present := resolveConfig("", " \n", false)
	if !present || cfg.Token != "environment-token" {
		t.Fatal("token normalization changed precedence")
	}
}
