package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRestishCLIUsesNexusCacheDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")

	cli := newRestishCLI()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "nexus-cli", "runtime.json")
	if got := cli.Paths.ConfigFile(); got != want {
		t.Fatalf("runtime state path = %q, want %q", got, want)
	}
}

func TestPublicHelpHidesEmbeddedEngineDetails(t *testing.T) {
	in := "Flags:\nGeneral Options\n  --help-all  Show all inherited Restish flags\n  --rsh-config string  config\n\nUse nexus-cli help\n"
	got := publicHelp(in)
	for _, hidden := range []string{"Restish", "restish", "--rsh-", "General Options"} {
		if strings.Contains(got, hidden) {
			t.Fatalf("public help contains %q:\n%s", hidden, got)
		}
	}
	if !strings.Contains(got, "Use nexus-cli help") {
		t.Fatalf("public help lost normal content:\n%s", got)
	}
}

func TestNewRestishCLIIgnoresStockRestishConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("RSH_CONFIG", filepath.Join(t.TempDir(), "restish.json"))
	t.Setenv("RSH_CONFIG_DIR", t.TempDir())

	cli := newRestishCLI()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "nexus-cli", "runtime.json")
	if got := cli.Paths.ConfigFile(); got != want {
		t.Fatalf("runtime state path = %q, want %q", got, want)
	}
}
