package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
	"tui.yamusic/internal/audio"
	"tui.yamusic/internal/logfile"
	"tui.yamusic/internal/music"

	tea "charm.land/bubbletea/v2"
)

type networkResult struct {
	serial int
	kind   string
	data   any
	err    error
}
type audioResult struct {
	revision   uint64
	generation uint64
	kind       string
	state      audio.State
	info       music.Stream
	err        error
}
type likeResult struct {
	index int
	liked bool
}

type loginResult struct {
	token music.Token
	uid   string
}
type catalogState struct {
	remote         []music.Playlist
	lists          []playlist
	list, selected int
	status         string
	loaded         bool
}

type liveModel struct {
	log                         *logfile.Log
	audioRevision               uint64
	pollBusy                    bool
	pollErrors                  int
	loadStarted                 time.Time
	lastPoll                    string
	spectrumEpoch               uint64
	spectrumBusy, spectrumTimer bool
	spectrumCancel              context.CancelFunc
	model
	vaultPath, vaultPassword string
	vaultUnlocked            bool
	ctx                      context.Context
	api                      music.Client
	audio                    audio.Backend
	uid, deviceID            string
	sections                 [3]catalogState
	remote                   []music.Playlist
	metadata                 []music.Track
	serial                   int
	cancel                   context.CancelFunc
	busy, audioBusy, loaded  bool
	audioCancel              context.CancelFunc
	generation               uint64
	code                     music.DeviceCode
	authDeadline             time.Time
	sessionToken             music.Token
	refreshAt                time.Time
	query                    string
	page                     int
}

func newLive(ctx context.Context, a music.Client, p audio.Backend) liveModel {
	m := newModel()
	m.live = true
	m.spectrumOn = true
	m.section = music.SectionCollections
	m.sectionStatus = "Загрузка…"
	m.library = nil
	m.lists = nil
	m.queue = nil
	m.notice = "Подключение к каталогу…"
	m.playback = "STOPPED"
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		m.notice = "Не удалось создать device ID"
	}
	return liveModel{model: m, ctx: ctx, api: a, audio: p, busy: true, deviceID: hex.EncodeToString(bytes)}
}
func (m liveModel) Init() tea.Cmd {
	if m.dialog == "unlock" {
		return tick()
	}
	a, ctx, serial, section, uid := m.api, m.ctx, m.serial, m.section, m.uid
	return tea.Batch(tick(), func() tea.Msg {
		lists, err := a.SectionCatalog(ctx, uid, section)
		return networkResult{serial, "catalog", lists, err}
	})
}
func (m *liveModel) request(kind string, fn func(context.Context, music.Client) (any, error)) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	m.serial++
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	serial, a := m.serial, m.api
	m.busy = true
	m.notice = "Запрос к сервису…"
	logger := m.log
	return func() tea.Msg {
		begin := time.Now()
		data, err := fn(ctx, a)
		logger.Record("request", 0, "", kind, err, time.Since(begin))
		return networkResult{serial, kind, data, err}
	}
}
func (m *liveModel) catalog() tea.Cmd {
	uid, section := m.uid, m.section
	m.sectionStatus = "Загрузка…"
	return m.request("catalog", func(ctx context.Context, a music.Client) (any, error) { return a.SectionCatalog(ctx, uid, section) })
}
func (m *liveModel) switchSection(section int) tea.Cmd {
	if m.busy && m.dialog != "" {
		return nil
	}
	if m.sectionStatus == "Загрузка…" {
		m.sectionStatus = ""
		m.sections[m.section].loaded = false
	}
	m.sections[m.section] = catalogState{m.remote, m.lists, m.list, m.selected, m.sectionStatus, m.sections[m.section].loaded}
	if m.cancel != nil {
		m.cancel()
	}
	m.serial++
	m.busy = false
	m.section = section
	state := m.sections[section]
	m.remote, m.lists, m.list, m.selected = state.remote, state.lists, state.list, state.selected
	m.sectionStatus = state.status
	m.focus = 0
	m.notice = "1 Личные · 2 Для вас · 3 Подборки · U обновить"
	if !state.loaded {
		return m.catalog()
	}
	return nil
}
func (m *liveModel) resetSections() {
	m.sections = [3]catalogState{}
	m.remote, m.lists = nil, nil
	m.list, m.selected = 0, 0
	m.sectionStatus = ""
}
func (m *liveModel) addTracks(tracks []music.Track) []int {
	ids := []int{}
	for _, tr := range tracks {
		if tr.ID == "" {
			continue
		}
		index := -1
		for i, existing := range m.metadata {
			if existing.ID == tr.ID && existing.AlbumID() == tr.AlbumID() {
				index = i
				break
			}
		}
		if index < 0 {
			index = len(m.library)
			m.library = append(m.library, displayTrack(tr))
			m.metadata = append(m.metadata, tr)
		}
		ids = append(ids, index)
	}
	return ids
}
func (m *liveModel) installPlaylist(p music.Playlist) {
	tracks := []music.Track{}
	for _, entry := range p.Tracks {
		if entry.Track != nil {
			tracks = append(tracks, *entry.Track)
		} else {
			tracks = append(tracks, music.Track{ID: entry.ID, Title: "Недоступный трек", Available: boolPointer(false)})
		}
	}
	ids := m.addTracks(tracks)
	for i, old := range m.remote {
		if old.Kind == p.Kind && old.Owner.UID == p.Owner.UID {
			m.remote[i] = p
			m.lists[i] = playlist{clean(p.Title), ids}
			if m.list == i {
				m.selected = max(0, min(m.selected, len(ids)-1))
				m.focus = 1
			}
			return
		}
	}
}
func boolPointer(v bool) *bool { return &v }
func (m *liveModel) openPlaylist() tea.Cmd {
	if m.list < 0 || m.list >= len(m.remote) {
		m.notice = "Выберите плейлист · U — каталог"
		return nil
	}
	p := m.remote[m.list]
	if p.Kind == "" || p.Owner.UID == "" {
		if p.Kind == "" {
			m.focus = 1
		}
		m.notice = "Это не плейлист · / — повторить поиск"
		return nil
	}
	return m.request("playlist", func(ctx context.Context, a music.Client) (any, error) { return a.Playlist(ctx, p) })
}
func (m *liveModel) startTrack(index int) tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	m.resetSpectrum()
	index = (index + len(m.queue)) % len(m.queue)
	tr := m.metadata[m.queue[index]]
	if tr.Available != nil && !*tr.Available {
		m.notice = "Трек недоступен в вашем регионе / по подписке"
		return nil
	}
	if m.spectrumHint != spectrumUnsupported {
		m.spectrumHint = ""
	}
	if m.audioCancel != nil {
		m.audioCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.audioCancel = cancel
	m.generation = m.audio.Advance()
	g, a, p := m.generation, m.api, m.audio
	volume, muted := m.volume, m.muted
	m.realDuration = 0
	m.current = index
	m.queueSelected = index
	for i, id := range m.tracks() {
		if m.metadata[id].ID == tr.ID {
			m.selected = i
			break
		}
	}
	m.pollBusy = false
	m.pollErrors = 0
	m.loadStarted = time.Now()
	m.lastPoll = ""
	m.log.Record("advance", g, string(tr.ID), "resolving", nil, 0)
	m.position = 0
	m.playing = false
	m.loaded = false
	m.audioBusy = true
	m.playback = "RESOLVING"
	m.streamQuality = ""
	logger := m.log
	return func() (result tea.Msg) {
		begin := time.Now()
		defer func() {
			if r, ok := result.(audioResult); ok {
				logger.Record("load_complete", g, string(tr.ID), "", r.err, time.Since(begin))
			}
		}()
		if _, err := p.Command(ctx, g, "stop"); err != nil {
			return audioResult{generation: g, kind: "load", err: err}
		}
		if meter, ok := p.(spectrumAudio); ok {
			cleanup, cancel := context.WithTimeout(ctx, spectrumTimeout)
			_, _ = meter.Spectrum(cleanup, g, false)
			cancel()
		}
		resolveStart := time.Now()
		info, err := a.Resolve(ctx, string(tr.ID))
		logger.Record("resolve", g, string(tr.ID), "", err, time.Since(resolveStart))
		if err != nil {
			return audioResult{generation: g, kind: "load", err: err}
		}
		if events, ok := p.(interface {
			Prepare(context.Context, uint64) error
		}); ok {
			if err = events.Prepare(ctx, g); err != nil {
				return audioResult{generation: g, kind: "load", err: err}
			}
		}
		for _, args := range [][]any{{"set_property", "volume", volume}, {"set_property", "mute", muted}, {"loadfile", info.URL, "replace"}, {"set_property", "pause", false}} {
			if _, err = p.Command(ctx, g, args...); err != nil {
				return audioResult{generation: g, kind: "load", err: err}
			}
		}
		return audioResult{generation: g, kind: "load", info: info}
	}
}
func (m *liveModel) audioCommand(args ...any) tea.Cmd {
	if m.audioBusy {
		m.notice = "Плеер выполняет команду…"
		return nil
	}
	m.audioBusy = true
	m.audioRevision++
	revision := m.audioRevision
	p, g, ctx := m.audio, m.generation, m.ctx
	return func() tea.Msg {
		_, err := p.Command(ctx, g, args...)
		return audioResult{generation: g, revision: revision, kind: "command", err: err}
	}
}
func (m *liveModel) stop() tea.Cmd {
	m.resetSpectrum()
	if m.audioCancel != nil {
		m.audioCancel()
	}
	m.generation = m.audio.Advance()
	m.audioBusy = false
	m.loaded = false
	m.playing = false
	m.position = 0
	m.playback = "STOPPED"
	return m.audioCommand("stop")
}
func (m *liveModel) loginPoll() tea.Cmd {
	code, deadline := m.code, m.authDeadline
	return m.request("token", func(ctx context.Context, a music.Client) (any, error) {
		timer := time.NewTimer(time.Duration(code.Interval) * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		if time.Now().After(deadline) {
			return nil, errors.New("код входа истёк: повторите A")
		}
		token, err := a.RequestToken(ctx, "device_code", code.Device)
		if err != nil {
			return nil, err
		}
		a = a.WithToken(token.Access)
		uid, err := a.Account(ctx)
		return loginResult{token, uid}, err
	})
}
func (m *liveModel) mutate(action, name string) tea.Cmd {
	if !m.account {
		m.notice = "Для изменения коллекции необходим вход (A)"
		return nil
	}
	var p music.Playlist
	if len(m.remote) > 0 {
		p = m.remote[m.list]
	}
	if action != "create" && string(p.Owner.UID) != m.uid {
		m.notice = "Выберите собственный плейлист"
		return nil
	}
	var tr music.Track
	if len(m.queue) > 0 {
		tr = m.metadata[m.queue[m.current]]
	}
	uid, index := m.uid, m.selected
	return m.request("mutated", func(ctx context.Context, a music.Client) (any, error) {
		return nil, a.Mutate(ctx, uid, action, name, p, tr, index)
	})
}
func (m *liveModel) search() tea.Cmd {
	query, page := m.query, m.page
	return m.request("search", func(ctx context.Context, a music.Client) (any, error) { return a.Search(ctx, query, page) })
}
func (m liveModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case spectrumTick:
		if msg.epoch != m.spectrumEpoch {
			return &m, nil
		}
		m.spectrumTimer = false
		return &m, m.readSpectrum()
	case spectrumResult:
		m.spectrumBusy = false
		if msg.epoch != m.spectrumEpoch || msg.generation != m.generation {
			return &m, m.scheduleSpectrum()
		}
		if errors.Is(msg.err, context.Canceled) || errors.Is(msg.err, context.DeadlineExceeded) || errors.Is(msg.err, audio.ErrSpectrumPending) {
			return &m, m.scheduleSpectrum()
		}
		if msg.err != nil {
			if m.spectrumHint == "" {
				m.log.Record("spectrum_error", m.generation, "", "retry", msg.err, 0)
			}
			m.spectrumLevels = [audio.SpectrumBands]float64{}
			m.spectrumHint = "lavfi/metadata недоступны"
			return &m, m.scheduleSpectrum()
		}
		if m.spectrumOn && m.playing && m.loaded {
			m.spectrumHint = ""
			m.spectrumLevels = msg.levels
		}
		return &m, m.scheduleSpectrum()
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case networkResult:
		if msg.serial != m.serial {
			return &m, nil
		}
		m.busy = false
		if msg.err != nil {
			var ae music.Error
			if msg.kind == "token" && errors.As(msg.err, &ae) {
				if ae.Code == "slow_down" {
					m.code.Interval += 5
				}
				if ae.Code == "authorization_pending" || ae.Code == "slow_down" {
					return &m, m.loginPoll()
				}
			}
			m.notice = msg.err.Error()
			if msg.kind == "catalog" {
				m.sectionStatus = "Ошибка: " + m.notice + " · U повторить"
			}
			if msg.kind == "unlocked" || msg.kind == "vault-saved" {
				m.authText = m.notice
				m.input = ""
			}
			if msg.kind == "device" || msg.kind == "token" {
				m.authText = m.notice + "\nEsc — закрыть, A — повторить"
				m.code = music.DeviceCode{}
			}
			if msg.kind == "refresh" {
				m.refreshAt = time.Now().Add(time.Minute)
			}
			return &m, nil
		}
		switch msg.kind {
		case "vault-saved":
			m.dialog = ""
			m.input = ""
			m.authText = ""
			return &m, m.catalog()
		case "unlocked":
			restored := msg.data.(restoredSession)
			login := restored.login
			m.api = m.api.WithToken(login.token.Access)
			m.sessionToken = login.token
			m.uid = login.uid
			m.account = true
			m.resetSections()
			m.refreshAt = restored.refreshAt
			m.vaultUnlocked = true
			m.dialog = ""
			m.input = ""
			return &m, m.persistVault()
		case "catalog":
			m.sections[m.section].loaded = true
			m.sectionStatus = ""
			if m.uid == "" && m.section != music.SectionCollections {
				m.sectionStatus = "Необходим вход (A)"
			}
			oldRemote, oldLists := m.remote, m.lists
			oldList, oldSelected := m.list, m.selected
			m.remote = append([]music.Playlist(nil), msg.data.([]music.Playlist)...)
			m.lists = nil
			m.list, m.selected = 0, 0
			for i, p := range m.remote {
				p.Tracks = nil
				p.Revision = 0
				list := playlist{name: clean(p.Title)}
				for j, old := range oldRemote {
					if old.Owner.UID != p.Owner.UID || old.Kind != p.Kind || p.Kind == "" || p.Owner.UID == "" {
						continue
					}
					if j < len(oldLists) && oldLists[j].tracks != nil {
						p = old
						p.Title = m.remote[i].Title
						list.tracks = oldLists[j].tracks
					}
					if j == oldList {
						m.list = i
						m.selected = max(0, min(oldSelected, len(list.tracks)-1))
					}
					break
				}
				m.remote[i] = p
				m.lists = append(m.lists, list)
			}
			m.notice = "Каталог обновлён · L — загрузить/обновить треки · Enter — открыть"
		case "playlist":
			m.installPlaylist(msg.data.(music.Playlist))
			m.notice = "Enter — играть; E — в очередь; / — поиск"
		case "search":
			tracks := msg.data.([]music.Track)
			ids := m.addTracks(tracks)
			p := music.Playlist{Title: "Поиск: " + m.query + " · " + strconv.Itoa(m.page+1)}
			index := -1
			for i, old := range m.remote {
				if old.Kind == "" {
					index = i
					break
				}
			}
			if index < 0 {
				index = len(m.remote)
				m.remote = append(m.remote, p)
				m.lists = append(m.lists, playlist{})
			}
			m.remote[index] = p
			m.lists[index] = playlist{p.Title, ids}
			m.list = index
			m.selected = 0
			m.focus = 1
			m.notice = "Поиск: [ / ] — страницы; Enter — играть; E — в очередь"
		case "device":
			m.code = msg.data.(music.DeviceCode)
			m.authDeadline = time.Now().Add(time.Duration(m.code.Expires) * time.Second)
			m.authText = "Откройте https://ya.ru/device\nКод: " + clean(m.code.User) + "\nОжидание подтверждения в браузере…\nПосле входа задайте пароль для шифрования токена."
			return &m, m.loginPoll()
		case "token", "refresh":
			login := msg.data.(loginResult)
			m.api = m.api.WithToken(login.token.Access)
			m.sessionToken = login.token
			m.uid = login.uid
			m.account = true
			if msg.kind == "token" {
				m.resetSections()
			}
			m.refreshAt = time.Now().Add(time.Duration(max(60, login.token.Expires-60)) * time.Second)
			m.dialog = ""
			m.code = music.DeviceCode{}
			m.authText = ""
			if msg.kind == "token" && m.vaultPath != "" {
				m.dialog = "encrypt"
				m.input = ""
				m.authText = ""
				return &m, nil
			}
			if m.vaultUnlocked {
				return &m, m.persistVault()
			}
			if msg.kind == "token" {
				return &m, m.catalog()
			}
		case "mutated":
			m.notice = "Изменения сохранены на сервере"
			return &m, m.catalog()
		case "liked":
			result := msg.data.(likeResult)
			m.likes[result.index] = result.liked
			m.notice = "Избранное обновлено на сервере"
		}
	case audioResult:
		id := ""
		if len(m.queue) > 0 {
			id = string(m.metadata[m.queue[m.current]].ID)
		}
		if msg.generation != m.generation {
			m.log.Record("stale", msg.generation, "", msg.kind, msg.err, 0)
			return &m, nil
		}
		if msg.kind == "poll" {
			m.pollBusy = false
			if msg.revision != m.audioRevision {
				return &m, nil
			}
		} else {
			m.audioBusy = false
			m.log.Record(msg.kind, m.generation, id, "complete", msg.err, 0)
		}
		if msg.err != nil && msg.kind == "poll" {
			m.pollErrors++
			if m.pollErrors == 1 {
				m.log.Record("poll_error", m.generation, id, "retry", msg.err, 0)
			}
			if m.pollErrors < 5 {
				return &m, nil
			}
		}
		if msg.err != nil {
			m.resetSpectrum()
			m.playing = false
			m.loaded = false
			m.playback = "ERROR"
			m.notice = msg.err.Error()
			return &m, nil
		}
		switch msg.kind {
		case "load":
			m.loaded = true
			m.playing = true
			m.playback = "BUFFERING"
			m.streamQuality = msg.info.Quality
			m.notice = "Поток получен; ожидание данных mpv"
		case "poll":
			m.pollErrors = 0
			if msg.state.Failed {
				m.loaded, m.playing = false, false
				m.playback = "ERROR"
				m.notice = "mpv: ошибка воспроизведения или event IPC"
				m.resetSpectrum()
				m.log.Record("end", m.generation, id, "error", errors.New("playback"), 0)
				return &m, nil
			}
			if msg.state.Idle && !msg.state.Ended {
				if time.Since(m.loadStarted) > 30*time.Second {
					m.loaded, m.playing = false, false
					m.playback = "ERROR"
					m.notice = "mpv: загрузка не началась / неожиданное idle"
					m.log.Record("end", m.generation, id, "unexpected_idle", errors.New("idle"), 0)
				}
				return &m, nil
			}
			m.position = int(msg.state.Position)
			m.realDuration = int(msg.state.Duration)
			m.playing = !msg.state.Paused && !msg.state.Idle && !msg.state.Ended
			m.playback = "PLAYING"
			if msg.state.Paused {
				m.playback = "PAUSED"
			}
			if msg.state.Buffering {
				m.playback = "BUFFERING"
			}
			if msg.state.Idle {
				m.playback = "STOPPED"
				m.loaded = false
			}
			if m.lastPoll != m.playback {
				m.log.Record("poll", m.generation, id, m.playback, nil, 0)
				m.lastPoll = m.playback
			}
			if msg.state.Ended {
				m.log.Record("end", m.generation, id, "eof", nil, 0)
				if m.current+1 < len(m.queue) {
					return &m, m.startTrack(m.current + 1)
				}
				return &m, m.stop()
			}
		}
		if !m.playing || !m.loaded || msg.state.Buffering && msg.kind == "poll" {
			m.resetSpectrum()
		}
		return &m, m.scheduleSpectrum()
	case tickMsg:
		cmds := []tea.Cmd{tick(), m.scheduleSpectrum()}
		if m.loaded && !m.audioBusy && !m.pollBusy {
			m.pollBusy = true
			p, g, ctx := m.audio, m.generation, m.ctx
			revision := m.audioRevision
			cmds = append(cmds, func() tea.Msg {
				pollCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				s, err := audio.Poll(pollCtx, p, g)
				return audioResult{generation: g, revision: revision, kind: "poll", state: s, err: err}
			})
		}
		if m.account && m.dialog == "" && !m.busy && m.sessionToken.Refresh != "" && time.Now().After(m.refreshAt) {
			refresh := m.sessionToken.Refresh
			cmds = append(cmds, m.request("refresh", func(ctx context.Context, a music.Client) (any, error) {
				token, err := a.RequestToken(ctx, "refresh_token", refresh)
				if err != nil {
					return nil, err
				}
				a = a.WithToken(token.Access)
				uid, err := a.Account(ctx)
				return loginResult{token, uid}, err
			}))
		}
		return &m, tea.Batch(cmds...)
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return &m, tea.Quit
		}
		if m.dialog != "" {
			return &m, m.handleLiveDialog(msg)
		}
		key = normalizeKey(key)
		switch key {
		case "n", "p", "s", "space", " ", "left", "right", "m", "+", "-", "enter", "v":
			m.log.Record("user_command", m.generation, "", key, nil, 0)
		}
		switch key {
		case "v":
			m.spectrumOn = !m.spectrumOn
			m.spectrumHint = ""
			if !m.spectrumOn {
				return &m, m.disableSpectrum()
			}
			return &m, m.scheduleSpectrum()
		case "1", "2", "3":
			return &m, m.switchSection(int(key[0] - '1'))
		case "q":
			return &m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % 3
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "?":
			m.dialog = "help"
		case "/":
			if !m.busy {
				m.dialog = "search"
				m.input = ""
			}
		case "u":
			if !m.busy {
				return &m, m.catalog()
			}
		case "l":
			if !m.busy {
				return &m, m.openPlaylist()
			}
		case "[", "]":
			if !m.busy && m.query != "" {
				if key == "]" {
					m.page++
				} else {
					m.page = max(0, m.page-1)
				}
				return &m, m.search()
			}
		case "a":
			if m.busy {
				m.notice = "Дождитесь запроса или Esc для отмены входа"
				break
			}
			m.dialog = "auth"
			if m.account {
				m.authText = "Выйти из аккаунта?\nТокен будет удалён из памяти и файла.\nОтзыв доступа: настройки Яндекс ID.\nEnter — выйти"
				break
			}
			m.authText = "Получение кода устройства…"
			id := m.deviceID
			return &m, m.request("device", func(ctx context.Context, a music.Client) (any, error) { return a.Device(ctx, id) })
		case "enter":
			if m.focus == 0 {
				if !m.busy {
					return &m, m.openPlaylist()
				}
				break
			}
			if m.focus == 1 && len(m.tracks()) > 0 {
				tr := m.metadata[m.tracks()[m.selected]]
				if tr.Available != nil && !*tr.Available {
					m.notice = "Трек недоступен"
					break
				}
				m.queue = append([]int(nil), m.tracks()...)
				return &m, m.startTrack(m.selected)
			}
			if m.focus == 2 && len(m.queue) > 0 {
				return &m, m.startTrack(m.queueSelected)
			}
		case "space", " ":
			if !m.loaded {
				return &m, m.startTrack(m.current)
			}
			paused := m.playing
			cmd := m.audioCommand("set_property", "pause", paused)
			if cmd != nil && paused {
				m.playing = false
				m.resetSpectrum()
			}
			return &m, cmd
		case "s":
			return &m, m.stop()
		case "n":
			return &m, m.startTrack(m.current + 1)
		case "p":
			return &m, m.startTrack(m.current - 1)
		case "left", "right":
			if m.loaded {
				delta := -5
				if key == "right" {
					delta = 5
				}
				return &m, m.audioCommand("seek", delta, "relative")
			}
		case "+", "=", "-":
			if m.audioBusy {
				break
			}
			delta := 5
			if key == "-" {
				delta = -5
			}
			m.volume = max(0, min(100, m.volume+delta))
			if m.loaded {
				return &m, m.audioCommand("set_property", "volume", m.volume)
			}
		case "m":
			if !m.audioBusy {
				m.muted = !m.muted
				if m.loaded {
					return &m, m.audioCommand("set_property", "mute", m.muted)
				}
			}
		case "e":
			if ids := m.tracks(); len(ids) > 0 {
				m.queue = append(m.queue, ids[m.selected])
				m.notice = "Добавлено в очередь"
			}
		case "c", "r", "d", "x", "i":
			if m.busy {
				break
			}
			if !m.account {
				m.notice = "Необходим вход (A)"
				break
			}
			if key == "c" {
				m.dialog = "create"
				m.input = ""
				break
			}
			if len(m.remote) == 0 || string(m.remote[m.list].Owner.UID) != m.uid {
				m.notice = "Выберите собственный плейлист"
				break
			}
			switch key {
			case "r":
				m.dialog = "rename"
				m.input = m.lists[m.list].name
			case "d":
				m.dialog = "delete"
			case "x":
				m.dialog = "remove"
			case "i":
				return &m, m.mutate("insert", "")
			}
		case "f":
			if !m.account {
				m.notice = "Необходим вход (A)"
				break
			}
			if m.busy || len(m.tracks()) == 0 {
				break
			}
			index := m.tracks()[m.selected]
			id := m.metadata[index].ID
			uid := m.uid
			return &m, m.request("liked", func(ctx context.Context, a music.Client) (any, error) {
				liked, err := a.ToggleLike(ctx, uid, id)
				return likeResult{index, liked}, err
			})
		}
	}
	return &m, nil
}
func (m *liveModel) handleLiveDialog(msg tea.KeyPressMsg) tea.Cmd {
	if m.dialog == "encrypt" || m.dialog == "unlock" {
		return m.passwordDialog(msg)
	}
	key := msg.String()
	if key == "esc" {
		if m.dialog == "auth" && !m.account {
			if m.cancel != nil {
				m.cancel()
			}
			m.serial++
			m.busy = false
			m.code = music.DeviceCode{}
			m.authText = ""
		}
		m.dialog = ""
		m.input = ""
		return nil
	}
	if m.dialog == "search" || m.dialog == "create" || m.dialog == "rename" {
		if key == "enter" {
			text := strings.TrimSpace(m.input)
			if text == "" {
				return nil
			}
			kind := m.dialog
			m.dialog = ""
			m.input = ""
			if kind == "search" {
				m.query = text
				m.page = 0
				return m.search()
			}
			return m.mutate(kind, text)
		}
		if key == "backspace" {
			r := []rune(m.input)
			if len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
		} else if msg.Text != "" && len([]rune(m.input+msg.Text)) <= 100 {
			m.input += clean(msg.Text)
		}
		return nil
	}
	if key != "enter" {
		return nil
	}
	switch m.dialog {
	case "auth":
		if !m.account {
			return nil
		}
		if err := m.forgetVault(); err != nil {
			m.authText = "Не удалось удалить файл токена: " + err.Error()
			return nil
		}
		m.account = false
		m.resetSections()
		m.section = music.SectionCollections
		m.uid = ""
		m.api = m.api.WithToken("")
		m.sessionToken = music.Token{}
		m.likes = map[int]bool{}
		m.queue = nil
		m.library = nil
		m.metadata = nil
		m.remote = nil
		m.lists = nil
		m.current = 0
		m.list = 0
		m.selected = 0
		m.queueSelected = 0
		m.dialog = ""
		return tea.Batch(m.stop(), m.catalog())
	case "delete", "remove":
		action := m.dialog
		m.dialog = ""
		return m.mutate(action, "")
	default:
		m.dialog = ""
	}
	return nil
}

func NewLive(ctx context.Context, client music.Client, player audio.Backend, log *logfile.Log, vaultPath string, spectrumEnabled bool) (tea.Model, error) {
	m := newLive(ctx, client, player)
	m.log, m.vaultPath = log, vaultPath
	m.spectrumOn = spectrumEnabled
	if _, err := os.Lstat(vaultPath); err == nil {
		m.dialog = "unlock"
		m.busy = false
		m.notice = "Введите пароль для сохранённого аккаунта"
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return m, nil
}
