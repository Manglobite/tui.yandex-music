package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"unicode"
)

func press(m model, code rune) model {
	next, _ := m.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	return next.(model)
}
func special(m model, code rune) model {
	next, _ := m.Update(tea.KeyPressMsg{Code: code})
	return next.(model)
}
func TestLayoutKeys(t *testing.T) {
	for _, r := range "fFаА" {
		m := newModel()
		m.account = true
		m = press(m, r)
		if !m.likes[0] {
			t.Fatalf("%c not mapped", r)
		}
	}
	en := []rune("qwertyuiopasdfghjklzxcvbnm")
	ru := []rune("йцукенгшщзфывапролдячсмить")
	for i, r := range ru {
		for _, v := range []rune{r, unicode.ToUpper(r), en[i], unicode.ToUpper(en[i])} {
			if normalizeKey(string(v)) != string(en[i]) {
				t.Fatalf("%c", v)
			}
		}
	}
}
func TestPlayer(t *testing.T) {
	m := newModel()
	m = special(m, tea.KeySpace)
	if !m.playing {
		t.Fatal("space")
	}
	m = press(m, 'n')
	if m.current != 1 {
		t.Fatal("next")
	}
	m = press(m, 'з')
	if m.current != 0 {
		t.Fatal("previous")
	}
	m = press(m, 's')
	if m.playing || m.position != 0 {
		t.Fatal("stop")
	}
	for range 30 {
		m = press(m, '+')
	}
	if m.volume != 100 {
		t.Fatal("max volume")
	}
	for range 30 {
		m = press(m, '-')
	}
	if m.volume != 0 {
		t.Fatal("min volume")
	}
	m = press(m, 'm')
	if !m.muted {
		t.Fatal("mute")
	}
	m.play(0)
	m.position = 29
	next, _ := m.Update(tickMsg{})
	m = next.(model)
	if m.current != 1 || m.position != 0 {
		t.Fatal("preview end")
	}
}
func TestPlaylistWorkflow(t *testing.T) {
	m := newModel()
	m = press(m, 'c')
	if m.dialog != "" {
		t.Fatal("guest mutation")
	}
	m = press(m, 'a')
	m = special(m, tea.KeyEnter)
	if !m.account {
		t.Fatal("demo account")
	}
	m = press(m, 'c')
	for _, r := range "Музыка Fа" {
		m = press(m, r)
	}
	if m.input != "Музыка Fа" {
		t.Fatal("input normalized")
	}
	m = special(m, tea.KeyEnter)
	if len(m.lists) != 4 || m.lists[3].name != "Музыка Fа" {
		t.Fatal("create")
	}
	m = press(m, 'i')
	if len(m.tracks()) != 1 {
		t.Fatal("add")
	}
	m = press(m, 'x')
	if len(m.tracks()) != 0 {
		t.Fatal("remove")
	}
	m = press(m, 'r')
	m.input = "New"
	m = special(m, tea.KeyEnter)
	if m.lists[3].name != "New" {
		t.Fatal("rename")
	}
	m = press(m, 'd')
	m = special(m, tea.KeyEscape)
	if len(m.lists) != 4 {
		t.Fatal("delete cancel")
	}
	for range 4 {
		m = press(m, 'd')
		m = special(m, tea.KeyEnter)
	}
	if len(m.lists) != 0 || len(m.queue) != 8 {
		t.Fatal("delete/queue independence")
	}
	m = press(m, 'x')
	m = special(m, tea.KeyEnter)
	_ = m.View()
}
func TestViews(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	{
		for _, size := range [][2]int{{120, 40}, {120, 32}, {100, 24}, {80, 24}, {60, 20}, {40, 12}, {1, 1}} {
			for _, dialog := range []string{"", "auth", "help", "create", "rename", "delete"} {
				m := newModel()
				m.width, m.height = size[0], size[1]
				m.dialog = dialog
				text := m.View().Content
				lines := strings.Split(text, "\n")
				if len(lines) > m.height {
					t.Fatalf("%v height %d", size, len(lines))
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("overflow %v: %s", size, line)
					}
				}
				if m.width >= 60 && dialog == "" && !strings.Contains(text, "Q выход") {
					t.Fatalf("footer missing %v", size)
				}
			}
		}
	}
}
func TestCollectionsTabsRender(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	{
		for section := range sectionNames {
			for _, width := range []int{64, 40, 33, 25} {
				m := newModel()
				m.live = true
				m.section = section
				lines := m.listLines(width, 10)
				text := ansi.Strip(m.panel("01 / COLLECTIONS", lines, width, 10, true))
				labels := []string{"1 — Личные", "2 — Для вас", "3 — Подборки"}
				if width < 44 {
					labels = []string{"1 Лич", "2 Вам", "3 Под"}
				}
				for i, label := range labels {
					if !strings.Contains(text, label) {
						t.Fatalf("section %d width %d: missing tab %q: %s", section, width, label, text)
					}
					if strings.Contains(text, "["+label+"]") != (i == section) {
						t.Fatalf("section %d width %d: incorrect selection for %q", section, width, label)
					}
				}
				rows := strings.Split(text, "\n")
				if !strings.Contains(rows[1], "COLLECTIONS") || !strings.Contains(rows[2], labels[section]) || !strings.Contains(rows[3], "U каталог · L треки") {
					t.Fatalf("incorrect header order: %s", text)
				}
				for _, row := range rows {
					if ansi.StringWidth(row) > width {
						t.Fatalf("width %d overflow: %s", width, row)
					}
				}
			}
		}
		m := newModel()
		text := ansi.Strip(m.panel("01 / COLLECTIONS", m.listLines(40, 10), 40, 10, true))
		if !strings.Contains(text, "Локальные образцы") || strings.Contains(text, "1 Лич") || strings.Contains(text, "1 — Личные") {
			t.Fatalf("demo catalog changed: %s", text)
		}
	}
}
func TestRemovedThemeKeyKeepsState(t *testing.T) {
	m := newModel()
	m.play(2)
	m.volume = 35
	m = press(m, 'Е')
	if m.current != 2 || m.volume != 35 {
		t.Fatal("theme resets state")
	}
}
