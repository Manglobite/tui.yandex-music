package tui

import (
	tea "charm.land/bubbletea/v2"
	lip "charm.land/lipgloss/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strconv"
	"strings"
)

type palette struct{ bg, fg, dim, accent, selection string }

func (m model) colors() palette {
	return palette{"#0B0B19", "#D6F5FA", "#6C8C9B", "#FCEE09", "#183D49"}
}
func (m model) style(fg, bg string) lip.Style {
	s := lip.NewStyle()
	if _, ok := os.LookupEnv("NO_COLOR"); !ok {
		s = s.Foreground(lip.Color(fg)).Background(lip.Color(bg))
	}
	return s
}
func fit(text string, width int) string { return ansi.Truncate(text, max(0, width), "") }
func (m model) line(text string, width int) string {
	c := m.colors()
	return m.style(c.fg, c.bg).Width(max(0, width)).Render(fit(text, width))
}
func (m model) panel(title string, lines []string, w, h int, active bool) string {
	c := m.colors()
	color := c.dim
	if active {
		color = c.accent
		title = "> " + title
	}
	inner := max(0, w-4)
	rows := []string{m.style(color, c.bg).Bold(true).Render(fit(title, inner))}
	for _, line := range lines {
		if len(rows) >= h-2 {
			break
		}
		rows = append(rows, fit(line, inner))
	}
	for len(rows) < h-2 {
		rows = append(rows, "")
	}
	s := m.style(c.fg, c.bg).Border(lip.NormalBorder()).Padding(0, 1).Width(w).Height(h)
	if _, ok := os.LookupEnv("NO_COLOR"); !ok {
		s = s.BorderForeground(lip.Color(color))
	}
	return s.Render(strings.Join(rows, "\n"))
}
func (m model) row(text string, selected bool, width int) string {
	c := m.colors()
	if selected {
		return m.style(c.accent, c.selection).Bold(true).Width(max(0, width)).Render(fit("> "+text, width))
	}
	return "  " + fit(text, max(0, width-2))
}
func (m model) sectionTabs(width int) string {
	labels := make([]string, len(sectionNames))
	for i, name := range sectionNames {
		labels[i] = strconv.Itoa(i+1) + " — " + name
	}
	if ansi.StringWidth(strings.Join(labels, "  "))+2 > width {
		labels = []string{"1 Лич", "2 Вам", "3 Под"}
	}
	c := m.colors()
	for i, label := range labels {
		if i == m.section {
			labels[i] = m.style(c.accent, c.selection).Bold(true).Render("[" + label + "]")
		} else {
			labels[i] = m.style(c.dim, c.bg).Render(label)
		}
	}
	return fit(strings.Join(labels, "  "), width)
}
func (m model) listLines(w, h int) []string {
	lines := []string{"Локальные образцы", ""}
	if m.live {
		lines = []string{m.sectionTabs(w - 4), "U каталог · L треки"}
		if m.sectionStatus != "" {
			lines = append(lines, m.sectionStatus)
		}
	}
	start := max(0, m.list-max(1, h-7)+1)
	for i := start; i < len(m.lists); i++ {
		p := m.lists[i]
		lines = append(lines, m.row(fmt.Sprintf("%s (%d)", p.name, len(p.tracks)), i == m.list, w-4))
	}
	if len(m.lists) == 0 {
		if m.live {
			if m.sectionStatus == "" {
				lines = append(lines, "Плейлистов пока нет", "U — обновить")
			}
		} else {
			lines = append(lines, "Пусто. C — создать")
		}
	}
	return lines
}
func (m model) trackLines(w, h int, queue bool) []string {
	ids, selected := m.tracks(), m.selected
	if queue {
		ids, selected = m.queue, m.queueSelected
	}
	if len(ids) == 0 {
		if m.live && !queue {
			return []string{"Нет загруженных треков", "L — загрузить/обновить", "U — обновить каталог"}
		}
		return []string{"Пока пусто", "I — добавить текущий трек"}
	}
	lines := []string{}
	start := max(0, selected-max(1, h-4)+1)
	for i := start; i < len(ids); i++ {
		t := m.library[ids[i]]
		mark := " "
		if m.likes[ids[i]] {
			mark = "*"
		}
		if queue && i == m.current {
			mark = ">"
		}
		text := fmt.Sprintf("%s %02d %s", mark, i+1, t.title)
		if w >= 48 {
			text = fmt.Sprintf("%s %02d %-24s %s", mark, i+1, t.title, clock(t.seconds))
		}
		lines = append(lines, m.row(text, i == selected, w-4))
	}
	return lines
}
func (m model) player(w, h int) string {
	status := "PAUSED / STOPPED"
	if m.playing {
		status = "PLAYING · SIMULATION"
	}
	t := track{title: "Выберите трек", artist: "Enter — воспроизвести"}
	if len(m.queue) > 0 && m.current < len(m.queue) {
		t = m.library[m.queue[m.current]]
	}
	if m.live {
		status = m.playback
	}
	vol := fmt.Sprintf("VOL %d%%", m.volume)
	if m.muted {
		vol += " · MUTED"
	}
	span := max(4, min(w-28, 54))
	filled := min(span, span*m.position/max(1, m.duration()))
	bar := strings.Repeat("━", filled) + strings.Repeat("─", span-filled)
	mode := "PREVIEW 00:30 · DEMO"
	if m.account {
		mode = "FULL LENGTH · DEMO"
	}
	if m.live {
		mode = "STREAM / " + m.streamQuality
	}
	return m.panel("NOW PLAYING // "+status, []string{
		t.title + " / " + t.artist,
		fmt.Sprintf("%s %s %s", clock(m.position), bar, clock(m.duration())),
		vol + "    " + mode,
		"P назад   Space пауза   N вперёд   S стоп   ←/→ ±5с",
	}, w, h, false)
}
func (m model) dialogView() string {
	title, text := "", ""
	switch m.dialog {
	case "auth":
		title = "ACCOUNT // Демонстрация входа"
		action := "Enter — включить демо-аккаунт"
		if m.account {
			action = "Enter — вернуться в гостевой режим"
		}
		text = "Реальный вход ещё не подключён.\nТокены и пароль здесь не запрашиваются.\n\nБудущий вход: OAuth Device Flow → код в браузере\n→ ожидание подтверждения → аккаунт.\n\nГость: поиск, публичные подборки, preview ~30с.\nАккаунт: личные плейлисты; полный трек зависит\nот прав, региона и подписки.\n\n" + action
	case "create":
		title = "CREATE // Новый плейлист"
		text = "Название (до 60 символов):\n\n> " + m.input + "_\n\nEnter — создать локально"
	case "rename":
		title = "RENAME // Переименовать"
		text = "Название (до 60 символов):\n\n> " + m.input + "_\n\nEnter — сохранить локально"
	case "delete":
		title = "DELETE // Подтверждение"
		text = "Удалить «" + m.lists[m.list].name + "»?\n\nТолько из демонстрационных данных.\nОчередь воспроизведения останется.\n\nEnter — удалить"
	case "help":
		title = "CONTROLS // EN + RU"
		text = "Tab / Shift+Tab — панели; ↑↓ / J K — выбор\nEnter — открыть / играть; Space — старт/пауза\nS — стоп; N / P — следующий / предыдущий\n← → — позиция ±5с; + − — громкость; M — mute\nA — вход/выход (демо)\nC / R / D — создать / переименовать / удалить\nI — текущий трек в выбранный плейлист\nX — убрать выбранный трек из плейлиста\nE — выбранный трек в очередь; F — избранное\nQ / Ctrl+C — выход; ? — справка\n\nF = f = а = А; аналогично все буквы EN/RU.\nВ полях ввода буквы не превращаются в команды.\nВсе изменения временные. Сети и звука нет."
	}
	if m.live {
		switch m.dialog {
		case "encrypt", "unlock":
			title = "TOKEN VAULT // AES-256-GCM"
			prompt := "Задайте пароль для шифрования токена"
			if m.dialog == "unlock" {
				prompt = "Введите пароль сохранённого токена"
			}
			text = prompt + "\nБез ограничений длины и сложности.\nПробелы и регистр сохраняются; пустой разрешён.\n\n> " + strings.Repeat("*", min(48, len([]rune(m.input)))) + "_\n\nEnter — продолжить\n" + m.authText
		case "auth":
			title = "ACCOUNT // YANDEX ID"
			text = m.authText
		case "search":
			title = "SEARCH // YANDEX MUSIC"
			text = "Название / исполнитель:\n\n> " + m.input + "_\n\nEnter — искать; [ / ] — страницы результатов"
		case "create", "rename":
			text = "Название (до 100 символов):\n\n> " + m.input + "_\n\nEnter — сохранить на сервере"
		case "delete":
			text = "Удалить «" + m.lists[m.list].name + "» на сервере?\n\nДействие необратимо.\nEnter — подтвердить; Esc — отменить"
		case "remove":
			title = "REMOVE // Подтверждение"
			text = "Убрать выбранный трек из плейлиста на сервере?\n\nEnter — подтвердить; Esc — отменить"
		case "help":
			text = strings.ReplaceAll(text, "A — вход/выход (демо)", "A — вход/выход; / — поиск; U — обновить")
			text = "1 — Личные; 2 — Для вас; 3 — Подборки\nЛичные и Для вас требуют входа (A).\nU — обновить каталог раздела; / — поиск\nL / Д — загрузить/обновить треки плейлиста\n" + strings.ReplaceAll(text, "Все изменения временные. Сети и звука нет.", "Плейлисты — на сервере. Токен — в шифрованном файле.")
		}
	}
	w := min(m.width, 70)
	lines := strings.Split(text, "\n")
	hint := "Esc — отмена / закрыть"
	if m.live && m.dialog == "encrypt" {
		hint = "Ctrl+C — выйти без сохранения"
	}
	if m.live && m.dialog == "unlock" {
		hint = "Esc — гостевой режим; файл останется"
	}
	lines = append(lines, "", hint)
	return lip.Place(m.width, m.height, lip.Center, lip.Center, m.panel(title, lines, w, min(m.height, len(lines)+3), true))
}
func (m model) View() tea.View { return m.view(true) }

func (m model) view(showMeter bool) tea.View {
	var content string
	c := m.colors()
	if m.width < 60 || m.height < 20 {
		content = "YAMUSIC / DEMO\nНужен терминал от 60×20.\nУвеличьте окно; Q — выход."
	} else if m.dialog != "" {
		content = m.dialogView()
	} else {
		brand := "Y A M U S I C  //"
		account := "GUEST · PREVIEW / A вход"
		if m.account {
			account = "DEMO ACCOUNT / A выход"
		}
		if m.live && m.account {
			account = "YANDEX ACCOUNT / A выход"
		}
		header := m.style(c.bg, c.accent).Bold(true).Width(m.width).Render(fit(" "+brand+" "+account, m.width))
		sub := m.line(" OFFLINE DESIGN LAB  /  нет подключения к Яндекс.Музыке", m.width)
		if m.live {
			sub = m.line(" U каталог · L треки · 1 Личные · 2 Для вас · 3 Подборки · / поиск · A вход", m.width)
		}
		meterHeight := 0
		if m.live && showMeter {
			meterHeight = 3
		}
		h := m.height - 12 - meterHeight
		var body string
		if m.width < 100 {
			title, lines := "01 / ПЛЕЙЛИСТЫ", m.listLines(m.width, h)
			if m.focus == 1 {
				title, lines = "02 / ТРЕКИ", m.trackLines(m.width, h, false)
			}
			if m.focus == 2 {
				title, lines = "03 / ОЧЕРЕДЬ", m.trackLines(m.width, h, true)
			}
			body = m.panel(title+" · Tab переключить", lines, m.width, h, true)

		} else {
			left := m.width / 3
			right := m.width - left
			upper := max(5, h/2)
			side := lip.JoinVertical(lip.Left, m.panel("01 / COLLECTIONS", m.listLines(left, upper), left, upper, m.focus == 0), m.panel("03 / NEXT IN LINE", m.trackLines(left, h-upper, true), left, h-upper, m.focus == 2))
			body = lip.JoinHorizontal(lip.Top, side, m.panel("02 / TRACK DATABASE", m.trackLines(right, h, false), right, h, m.focus == 1))
		}
		player := m.player(m.width, 7)
		if m.live && showMeter {
			player = lip.JoinVertical(lip.Left, player, m.spectrumView())
		}
		content = lip.JoinVertical(lip.Left, header, sub, player, body)
		content = lip.JoinVertical(lip.Left, content, m.line(m.notice, m.width), m.line(" Tab панели · ↑↓ выбор · Enter играть · Space пауза · N/P трек · +/- VOL", m.width), m.line(" Q выход · ? помощь · A вход · C/R/D плейлист", m.width))
	}
	rows := strings.Split(content, "\n")
	if len(rows) > m.height {
		rows = rows[:m.height]
	}
	for i := range rows {
		rows[i] = fit(rows[i], m.width)
	}
	content = strings.Join(rows, "\n")
	v := tea.NewView(content)
	if _, noColor := os.LookupEnv("NO_COLOR"); !noColor {
		bg := lip.Color(c.bg)
		canvas := lip.NewCanvas(max(0, m.width), max(0, m.height))
		canvas.Compose(lip.NewLayer(content))
		for y := range canvas.Height() {
			for x := range canvas.Width() {
				cell := canvas.CellAt(x, y)
				if cell.Style.Bg == nil {
					cell.Style.Bg = bg
					canvas.SetCell(x, y, cell)
				}
			}
		}
		v.SetContent(canvas.Render())
		v.BackgroundColor = bg
	}
	v.AltScreen = true
	v.WindowTitle = "Yamusic · design prototype"
	return v
}
