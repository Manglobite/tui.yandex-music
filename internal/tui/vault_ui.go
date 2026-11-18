package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"os"
	"time"
	"tui.yamusic/internal/music"
	"tui.yamusic/internal/vault"
)

func (m *liveModel) persistVault() tea.Cmd {
	path, password := m.vaultPath, m.vaultPassword
	session := vault.Session{Token: m.sessionToken, RefreshAt: m.refreshAt}
	return m.request("vault-saved", func(_ context.Context, _ music.Client) (any, error) { return nil, vault.Save(path, password, session) })
}
func (m *liveModel) passwordDialog(msg tea.KeyPressMsg) tea.Cmd {
	if m.busy {
		return nil
	}
	switch msg.String() {
	case "esc":
		if m.dialog == "unlock" {
			m.dialog = ""
			m.input = ""
			m.vaultPassword = ""
			m.authText = ""
			return m.catalog()
		}
		return nil
	case "backspace":
		r := []rune(m.input)
		if len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case "enter":
		m.vaultPassword = m.input
		m.input = ""
		m.authText = "Обработка…"
		if m.dialog == "encrypt" {
			m.vaultUnlocked = true
			return m.persistVault()
		}
		path, password := m.vaultPath, m.vaultPassword
		return m.request("unlocked", func(ctx context.Context, a music.Client) (any, error) {
			session, err := vault.Load(path, password)
			if err != nil {
				return nil, err
			}
			token := session.Token
			if time.Now().After(session.RefreshAt) && token.Refresh != "" {
				token, err = a.RequestToken(ctx, "refresh_token", token.Refresh)
				if err != nil {
					return nil, err
				}
				if token.Refresh == "" {
					token.Refresh = session.Token.Refresh
				}
				session = vault.Session{Token: token, RefreshAt: time.Now().Add(time.Duration(max(60, token.Expires-60)) * time.Second)}
				if err = vault.Save(path, password, session); err != nil {
					return nil, err
				}
			}
			a = a.WithToken(token.Access)
			uid, err := a.Account(ctx)
			return restoredSession{loginResult{token, uid}, session.RefreshAt}, err
		})
	default:
		m.input += msg.Text
	}
	return nil
}

type restoredSession struct {
	login     loginResult
	refreshAt time.Time
}

func (m *liveModel) forgetVault() error {
	if m.vaultPath != "" {
		if err := os.Remove(m.vaultPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	m.vaultPassword = ""
	m.vaultUnlocked = false
	return nil
}
