package logfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlaybackLogSafety(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		l, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		l.Record("resolve", 1, "https://secret/token", "", errors.New("password=SECRET OAuth token client_secret headers"), time.Second)
		name := l.file.Name()
		l.Close()
		l.Close()
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("permissions", err)
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"SECRET", "https://", "password", "client_secret", "OAuth", "headers"} {
			if strings.Contains(string(raw), s) {
				t.Fatal("leak", s)
			}
		}
		var record playbackRecord
		if json.Unmarshal(raw, &record) != nil || record.Error != "operation_failed" || record.Track != "redacted" {
			t.Fatal(string(raw))
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("not one unique file per open", err)
	}
	if _, err = Open(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
}
