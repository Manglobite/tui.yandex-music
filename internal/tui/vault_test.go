package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tui.yamusic/internal/music"
	"tui.yamusic/internal/vault"
)

func TestLogoutRemovesVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.enc")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	m.vaultPath = path
	if err := m.forgetVault(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("logout retained token")
	}
}

func TestPasswordUI(t *testing.T) {
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	m.busy = false
	m.dialog = "encrypt"
	m.vaultPath = filepath.Join(t.TempDir(), "token.enc")
	m.sessionToken = music.Token{Access: "secret"}
	password := "  аАfF  "
	for _, r := range password {
		m.passwordDialog(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.input != password {
		t.Fatal("password normalized")
	}
	if strings.Contains(m.View().Content, password) {
		t.Fatal("password visible")
	}
	cmd := m.passwordDialog(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("missing save")
	}
	result := cmd().(networkResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if _, err := vault.Load(m.vaultPath, password); err != nil {
		t.Fatal(err)
	}
}
