package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runPreview(t *testing.T) {
	t.Helper()
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = oldArgs, oldFlags }()
	flag.CommandLine = flag.NewFlagSet("preview-test", flag.ContinueOnError)
	os.Args = []string{"yamusic", "-preview", "-width", "1", "-height", "1"}
	if err := run(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewLoggingDisabled(t *testing.T) {
	for _, value := range []string{"", "false"} {
		t.Run("YAMUSIC_LOGS="+value, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("YAMUSIC_LOGS", value)
			runPreview(t)
			if _, err := os.Stat("logs"); !os.IsNotExist(err) {
				t.Fatal("logs/ must not be created when logging is disabled", err)
			}
		})
	}
}

func TestPreviewOneLogPerRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("YAMUSIC_LOGS", "true")
	for i := 0; i < 2; i++ {
		runPreview(t)
	}
	rootLogs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(rootLogs) != 0 {
		t.Fatal("logs created outside logs/", rootLogs, err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "logs", "*.log"))
	if err != nil || len(files) != 2 {
		t.Fatal("one unique log per launch", files, err)
	}
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(raw), `"stage":"start"`) != 1 || strings.Count(string(raw), `"stage":"exit"`) != 1 {
			t.Fatal(string(raw))
		}
	}
}
