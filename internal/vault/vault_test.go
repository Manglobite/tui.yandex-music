package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
	"tui.yamusic/internal/music"
)

func TestVaultPasswords(t *testing.T) {
	session := Session{music.Token{Access: "access-secret", Refresh: "refresh-secret", Expires: 3600}, time.Now()}
	for _, password := range []string{"", "a", " аА F \t", " пароль "} {
		data, err := sealSession(password, session)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(session.Token.Access)) {
			t.Fatal("plaintext token")
		}
		got, err := openSession(password, data)
		if err != nil || got.Token != session.Token {
			t.Fatalf("roundtrip: %v", err)
		}
		if _, err = openSession(password+"x", data); err == nil {
			t.Fatal("wrong password accepted")
		}
		data[len(data)-1] ^= 1
		if _, err = openSession(password, data); err == nil {
			t.Fatal("tamper accepted")
		}
	}
}
func TestVaultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials", "token.enc")
	session := Session{Token: music.Token{Access: "secret"}}
	if err := Save(path, "", session); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path, "bad"); err == nil {
		t.Fatal("wrong password")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed unlock modified file")
	}
}
