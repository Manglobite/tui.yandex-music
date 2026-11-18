package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strings"
	"time"
	"tui.yamusic/internal/audio"
	"tui.yamusic/internal/music"
	"unicode"
)

type track struct {
	title, artist string
	seconds       int
}
type playlist struct {
	name   string
	tracks []int
}

var sectionNames = [...]string{"Личные", "Для вас", "Подборки"}

type tickMsg struct{}
type model struct {
	spectrumOn                        bool
	spectrumLevels                    [audio.SpectrumBands]float64
	spectrumHint                      string
	section                           int
	sectionStatus                     string
	live                              bool
	playback, streamQuality, authText string
	realDuration                      int
	width, height, focus              int
	lists                             []playlist
	library                           []track
	list, selected, queueSelected     int
	queue                             []int
	current, position, volume         int
	playing, muted, account           bool
	likes                             map[int]bool
	dialog, input, notice             string
}

func newModel() model {
	return model{
		width: 120, height: 32, volume: 65,
		library: []track{
			{"Wasteland Frequency", "Vault Radio", 214},
			{"Neon After Midnight", "Night City Signals", 186},
			{"Тихий город", "Северный эфир", 243},
			{"Atomic Sunset", "The Wanderers", 198},
			{"Chrome Heart", "Low Orbit", 227},
			{"Последний поезд", "Северный эфир", 205},
			{"Ghost in the Radio", "Night City Signals", 174},
			{"Home, Somewhere", "Vault Radio", 261},
		},
		lists: []playlist{{"Плейлист дня · демо", []int{0, 1, 2, 3, 4, 5, 6, 7}}, {"Ночная поездка", []int{1, 4, 6}}, {"Тёплый эфир", []int{0, 2, 3, 7}}},
		queue: []int{0, 1, 2, 3, 4, 5, 6, 7}, likes: map[int]bool{},
		notice: "ДЕМО: вымышленные треки, без сети и звука. ? — все команды.",
	}
}

func tick() tea.Cmd           { return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }
func (m model) Init() tea.Cmd { return tick() }
func normalizeKey(key string) string {
	runes := []rune(key)
	if len(runes) != 1 {
		return key
	}
	r := unicode.ToLower(runes[0])
	ru := []rune("йцукенгшщзхъфывапролджэячсмитьбюё")
	en := []rune("qwertyuiop[]asdfghjkl;'zxcvbnm,.`")
	for i, c := range ru {
		if r == c {
			return string(en[i])
		}
	}
	return string(r)
}
func (m model) tracks() []int {
	if len(m.lists) == 0 {
		return nil
	}
	return m.lists[m.list].tracks
}
func (m *model) play(index int) {
	if len(m.queue) == 0 {
		return
	}
	m.current = (index + len(m.queue)) % len(m.queue)
	m.queueSelected = m.current
	for i, id := range m.tracks() {
		if id == m.queue[m.current] {
			m.selected = i
			break
		}
	}
	m.position, m.playing = 0, true
}
func (m model) duration() int {
	if m.live {
		return m.realDuration
	}
	if len(m.queue) == 0 {
		return 0
	}
	if !m.account {
		return 30
	}
	return m.library[m.queue[m.current]].seconds
}
func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case tickMsg:
		if m.playing {
			m.position++
			if m.position >= m.duration() {
				if m.current+1 < len(m.queue) {
					m.play(m.current + 1)
				} else {
					m.playing = false
					m.position = 0
				}
			}
		}
		return m, tick()
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.dialog != "" {
			m.handleDialog(msg)
			return m, nil
		}
		switch normalizeKey(key) {
		case "q":
			return m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % 3
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "enter":
			switch m.focus {
			case 0:
				m.focus = 1
			case 1:
				if ids := m.tracks(); len(ids) > 0 {
					m.queue = append([]int(nil), ids...)
					m.play(m.selected)
					m.queueSelected = m.current
				}
			case 2:
				m.play(m.queueSelected)
			}
		case "space", " ":
			if len(m.queue) > 0 {
				m.playing = !m.playing
			}
		case "s":
			m.playing = false
			m.position = 0
		case "n":
			m.play(m.current + 1)
		case "p":
			m.play(m.current - 1)
		case "left":
			m.position = max(0, m.position-5)
		case "right":
			m.position = max(0, min(m.duration()-1, m.position+5))
		case "+", "=":
			m.volume = min(100, m.volume+5)
		case "-":
			m.volume = max(0, m.volume-5)
		case "m":
			m.muted = !m.muted
		case "a":
			m.dialog = "auth"
		case "?":
			m.dialog = "help"
		case "f":
			if !m.account {
				m.notice = "Избранное требует входа: A — демо аккаунта."
				break
			}
			if ids := m.tracks(); len(ids) > 0 {
				id := ids[m.selected]
				m.likes[id] = !m.likes[id]
			}
		case "e":
			if ids := m.tracks(); len(ids) > 0 {
				m.queue = append(m.queue, ids[m.selected])
				m.notice = "Трек добавлен в конец очереди (локально)."
			}
		case "c", "r", "d", "x":
			if !m.account {
				m.notice = "Изменение плейлистов требует входа: A."
				break
			}
			switch normalizeKey(key) {
			case "c":
				m.dialog = "create"
				m.input = ""
			case "r":
				if len(m.lists) > 0 {
					m.dialog = "rename"
					m.input = m.lists[m.list].name
				}
			case "d":
				if len(m.lists) > 0 {
					m.dialog = "delete"
				}
			case "x":
				if ids := m.tracks(); len(ids) > 0 {
					m.lists[m.list].tracks = append(ids[:m.selected:m.selected], ids[m.selected+1:]...)
					m.selected = max(0, min(m.selected, len(ids)-2))
				}
			}
		case "i":
			if !m.account {
				m.notice = "Добавление в плейлист требует входа: A."
				break
			}
			if len(m.lists) > 0 && len(m.queue) > 0 {
				m.lists[m.list].tracks = append(m.lists[m.list].tracks, m.queue[m.current])
				m.notice = "Текущий трек добавлен в выбранный плейлист."
			}
		}
	}
	return m, nil
}
func (m *model) move(delta int) {
	switch m.focus {
	case 0:
		m.list = max(0, min(len(m.lists)-1, m.list+delta))
		m.selected = 0
	case 1:
		m.selected = max(0, min(len(m.tracks())-1, m.selected+delta))
	case 2:
		m.queueSelected = max(0, min(len(m.queue)-1, m.queueSelected+delta))
	}
}
func (m *model) handleDialog(msg tea.KeyPressMsg) {
	key := msg.String()
	if key == "esc" {
		m.dialog = ""
		m.input = ""
		return
	}
	if m.dialog == "create" || m.dialog == "rename" {
		switch key {
		case "enter":
			name := strings.TrimSpace(m.input)
			if name == "" {
				return
			}
			if m.dialog == "create" {
				m.lists = append(m.lists, playlist{name: name})
				m.list = len(m.lists) - 1
				m.selected = 0
			} else {
				m.lists[m.list].name = name
			}
			m.dialog = ""
			m.input = ""
			m.notice = "Изменения только в памяти; после выхода сбрасываются."
		case "backspace":
			r := []rune(m.input)
			if len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" && len([]rune(m.input))+len([]rune(msg.Text)) <= 60 {
				m.input += msg.Text
			}
		}
		return
	}
	if key != "enter" {
		return
	}
	switch m.dialog {
	case "auth":
		m.account = !m.account
		m.playing = false
		m.position = 0
		m.notice = "Режим аккаунта переключён. OAuth НЕ выполнялся: это UI-макет."
	case "delete":
		m.lists = append(m.lists[:m.list:m.list], m.lists[m.list+1:]...)
		m.list = max(0, min(m.list, len(m.lists)-1))
		m.selected = 0
		m.notice = "Плейлист удалён только из демо; очередь сохранена."
	}
	m.dialog = ""
}
func clock(seconds int) string { return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60) }

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
func displayTrack(t music.Track) track {
	names := []string{}
	for _, a := range t.Artists {
		names = append(names, clean(a.Name))
	}
	return track{clean(t.Title), strings.Join(names, ", "), t.Duration / 1000}
}

func NewDemo(width, height int) tea.Model {
	m := newModel()
	m.width, m.height = max(1, width), max(1, height)
	return m
}
