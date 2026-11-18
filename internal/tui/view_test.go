package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"

	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func enableViewColors(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
}

func sameViewColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestViewOpaqueViewport(t *testing.T) {
	enableViewColors(t)
	{
		for _, size := range [][2]int{{120, 40}, {101, 31}, {100, 24}, {80, 24}, {60, 20}, {59, 19}, {40, 12}, {2, 2}, {1, 1}} {
			for _, mode := range []string{"demo", "dialog", "live", "spectrum"} {
				t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], mode), func(t *testing.T) {
					m := newModel()
					m.width, m.height = size[0], size[1]
					m.live = mode == "live" || mode == "spectrum"
					if mode == "dialog" {
						m.dialog = "help"
					}
					if mode == "spectrum" {
						m.spectrumOn = true
					}
					v := m.View()
					c := m.colors()
					bg, selection, accent := lip.Color(c.bg), lip.Color(c.selection), lip.Color(c.accent)
					if !v.AltScreen || !sameViewColor(v.BackgroundColor, bg) {
						t.Fatal("missing full-screen background")
					}
					rows := strings.Split(v.Content, "\n")
					if len(rows) != m.height {
						t.Fatalf("height = %d, want %d", len(rows), m.height)
					}
					for y, row := range rows {
						if ansi.StringWidth(row) != m.width {
							t.Fatalf("row %d width = %d, want %d", y, ansi.StringWidth(row), m.width)
						}
					}
					canvas := lip.NewCanvas(m.width, m.height).Compose(lip.NewLayer(v.Content))
					selected := false
					for y := range m.height {
						for x := range m.width {
							cell := canvas.CellAt(x, y)
							if !sameViewColor(cell.Style.Bg, bg) && !sameViewColor(cell.Style.Bg, selection) && !sameViewColor(cell.Style.Bg, accent) {
								t.Fatalf("unexpected/transparent background at %d,%d: %v", x, y, cell.Style.Bg)
							}
							selected = selected || sameViewColor(cell.Style.Bg, selection)
						}
					}
					if m.width >= 60 && m.height >= 20 && mode != "dialog" && !selected {
						t.Fatal("selection background lost")
					}
				})
			}
		}
	}
}

func TestViewNestedResetsAndPadding(t *testing.T) {
	enableViewColors(t)
	for _, reset := range []string{"\x1b[m", "\x1b[0m", "\x1b[49m", "\x1b[0;39;49m"} {
		t.Run(fmt.Sprintf("%q", reset), func(t *testing.T) {
			m := newModel()
			m.width, m.height = 120, 40
			m.notice = lip.NewStyle().Foreground(lip.Color("#FF00FF")).Render("nested") + reset + "  gap"
			v := m.View()
			canvas := lip.NewCanvas(m.width, m.height).Compose(lip.NewLayer(v.Content))
			bg := lip.Color(m.colors().bg)
			for _, point := range [][2]int{{1, 3}, {118, 5}, {119, 39}, {0, 39}} {
				if !sameViewColor(canvas.CellAt(point[0], point[1]).Style.Bg, bg) {
					t.Fatalf("padding/border/empty cell transparent at %v", point)
				}
			}
			for y, row := range strings.Split(ansi.Strip(v.Content), "\n") {
				if !strings.Contains(row, "nested  gap") {
					continue
				}
				for x := range m.width {
					if !sameViewColor(canvas.CellAt(x, y).Style.Bg, bg) {
						t.Fatalf("nested reset leaked at %d,%d", x, y)
					}
				}
				return
			}
			t.Fatal("notice missing")
		})
	}
}

func TestViewNoColorBackground(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := newModel()
	v := m.View()
	if v.BackgroundColor != nil {
		t.Fatal("NO_COLOR must not set terminal background")
	}
	canvas := lip.NewCanvas(m.width, m.height).Compose(lip.NewLayer(v.Content))
	for y := range m.height {
		for x := range m.width {
			if canvas.CellAt(x, y).Style.Bg != nil {
				t.Fatalf("NO_COLOR background at %d,%d", x, y)
			}
		}
	}
}
