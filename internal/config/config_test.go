package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, key := range []string{ClientID, ClientSecret, StreamKey, Logs, Spectrum} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), ".env")
	if cfg, err := Load(path); err != nil || cfg != (Config{SpectrumEnabled: true}) {
		t.Fatal("missing file must be optional", err)
	}
	if err := os.WriteFile(path, []byte("# Local settings\n\nYAMUSIC_CLIENT_ID=file-id\nYAMUSIC_CLIENT_SECRET=example=value\nYAMUSIC_STREAM_KEY=file-key\nYAMUSIC_LOGS=true\nYAMUSIC_SPECTRUM=false\nOTHER=ignored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ClientID, " env-id ")
	t.Setenv(StreamKey, "")
	want := Config{ClientID: "env-id", ClientSecret: "example=value", LogsEnabled: true}
	if cfg, err := Load(path); err != nil || cfg != want {
		t.Fatal("configuration or precedence mismatch", err)
	}
	t.Setenv(Logs, "false")
	t.Setenv(Spectrum, "true")
	if cfg, err := Load(path); err != nil || cfg.LogsEnabled || !cfg.SpectrumEnabled {
		t.Fatal("process environment must override .env options", err)
	}
}

func TestInvalidLineDoesNotExposeContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# Settings\nprivate-test-data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "2") || strings.Contains(err.Error(), "private-test-data") {
		t.Fatal("expected a redacted error with line number")
	}
}
