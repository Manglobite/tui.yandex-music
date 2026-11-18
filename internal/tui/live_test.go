package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"tui.yamusic/internal/music"

	tea "charm.land/bubbletea/v2"
)

type fakeAudio struct {
	generation uint64
	calls      [][]any
}

func (p *fakeAudio) Generation() uint64 { return p.generation }
func (p *fakeAudio) Advance() uint64    { p.generation++; return p.generation }
func (p *fakeAudio) Close()             {}
func (p *fakeAudio) Command(_ context.Context, g uint64, args ...any) (json.RawMessage, error) {
	if g != p.generation {
		return nil, context.Canceled
	}
	p.calls = append(p.calls, args)
	return json.RawMessage(`false`), nil
}
func testAPI(t *testing.T, handler http.HandlerFunc) music.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return music.NewWithEndpoints(music.Credentials{ClientID: "test-client", ClientSecret: "test-secret", StreamKey: "test-stream"}, server.URL, server.URL)
}
func TestLiveCommandStateAndStaleResults(t *testing.T) {
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	m.busy = false
	m.dialog = "auth"
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	n := next.(*liveModel)
	if cmd != nil || n.dialog != "" || n.serial != 1 {
		t.Fatal("cancel")
	}
	next, _ = n.Update(networkResult{serial: 0, kind: "token", data: loginResult{music.Token{Access: "stale"}, "1"}})
	if next.(*liveModel).account {
		t.Fatal("stale auth accepted")
	}
	n = next.(*liveModel)
	n.dialog = "search"
	n.input = "test"
	next, cmd = n.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	n = next.(*liveModel)
	if cmd == nil || !n.busy || n.serial != 2 || n.query != "test" {
		t.Fatal("command mutation lost")
	}
	next, _ = n.Update(audioResult{generation: 99, kind: "load", info: music.Stream{Quality: "hq"}})
	if next.(*liveModel).loaded {
		t.Fatal("stale audio accepted")
	}
}
func TestLiveEmptyViews(t *testing.T) {
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	for _, dialog := range []string{"", "auth", "search", "help"} {
		m.dialog = dialog
		if m.View().Content == "" {
			t.Fatal("empty view")
		}
	}
}
func TestLiveSections(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"forbidden"}`))
		case "/landing3":
			_, _ = w.Write([]byte(`{"result":{"blocks":[]}}`))
		case "/users/7/playlists/list":
			_, _ = w.Write([]byte(`{"result":[{"kind":1,"title":"Mine","owner":{"uid":7}}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	m := newLive(context.Background(), a, &fakeAudio{})
	m.uid, m.account, m.busy = "7", true, false
	m.queue = []int{0}
	m.library = []track{{title: "Queued"}}
	m.metadata = []music.Track{{ID: "1"}}
	var cmd tea.Cmd
	next, cmd := m.Update(tea.KeyPressMsg{Code: '1'})
	n := next.(*liveModel)
	next, _ = n.Update(cmd())
	n = next.(*liveModel)
	if len(n.remote) != 1 || n.remote[0].Title != "Mine" {
		t.Fatal("personal catalog")
	}
	next, cmd = n.Update(tea.KeyPressMsg{Code: '2'})
	n = next.(*liveModel)
	next, _ = n.Update(cmd())
	n = next.(*liveModel)
	if n.busy || !strings.Contains(n.sectionStatus, "Ошибка") || len(n.remote) != 0 {
		t.Fatal("section error")
	}
	next, cmd = n.Update(tea.KeyPressMsg{Code: '3'})
	n = next.(*liveModel)
	stale := networkResult{serial: n.serial - 1, kind: "catalog", data: []music.Playlist{{Title: "Stale"}}}
	next, _ = n.Update(stale)
	n = next.(*liveModel)
	next, _ = n.Update(cmd())
	n = next.(*liveModel)
	if len(n.remote) != 0 || !strings.Contains(strings.Join(n.listLines(60, 20), "\n"), "Плейлистов пока нет") {
		t.Fatal("empty section")
	}
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}} {
		n.width, n.height = size[0], size[1]
		if !strings.Contains(n.View().Content, "2 Для вас") {
			t.Fatal("section controls missing")
		}
	}
	next, cmd = n.Update(tea.KeyPressMsg{Code: '1'})
	n = next.(*liveModel)
	if cmd != nil || len(n.remote) != 1 || n.remote[0].Title != "Mine" || len(n.queue) != 1 {
		t.Fatal("section isolation / queue")
	}
	next, cmd = n.Update(tea.KeyPressMsg{Code: 'u'})
	if cmd == nil || !next.(*liveModel).busy {
		t.Fatal("refresh section")
	}
}

func TestSectionRefreshFailureAndCancellation(t *testing.T) {
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	m.busy = false
	m.remote = []music.Playlist{{Kind: "1", Title: "Cached"}}
	m.lists = []playlist{{name: "Cached"}}
	m.sections[m.section].loaded = true
	m.list, m.selected, m.focus = 0, 0, 0
	_ = m.catalog()
	next, _ := m.Update(networkResult{serial: m.serial, kind: "catalog", err: errors.New("offline")})
	n := next.(*liveModel)
	if len(n.lists) != 1 || n.lists[0].name != "Cached" || !strings.Contains(n.sectionStatus, "offline") {
		t.Fatal("failed refresh lost cached catalog")
	}
	_ = n.catalog()
	serial := n.serial
	_ = n.switchSection(music.SectionPersonal)
	if n.sections[music.SectionCollections].loaded {
		t.Fatal("canceled refresh marked loaded")
	}
	_ = n.switchSection(music.SectionCollections)
	if !n.busy || n.serial <= serial {
		t.Fatal("canceled section did not reload")
	}
	next, _ = n.Update(networkResult{serial: serial, kind: "catalog", data: []music.Playlist{{Title: "Old"}}})
	n = next.(*liveModel)
	if len(n.remote) != 1 || n.remote[0].Title != "Cached" || !n.busy {
		t.Fatal("stale result changed section")
	}
	n.resetSections()
	if len(n.remote) != 0 || len(n.lists) != 0 || n.sections[music.SectionCollections].loaded {
		t.Fatal("account reset retained sections")
	}
}

func TestGuestSections(t *testing.T) {
	a := testAPI(t, func(_ http.ResponseWriter, r *http.Request) { t.Errorf("guest private request %s", r.URL.Path) })
	m := newLive(context.Background(), a, &fakeAudio{})
	for _, key := range []rune{'1', '2'} {
		next, cmd := m.Update(tea.KeyPressMsg{Code: key})
		n := next.(*liveModel)
		next, _ = n.Update(cmd())
		m = *next.(*liveModel)
		if m.busy || len(m.lists) != 0 || !strings.Contains(m.sectionStatus, "вход") {
			t.Fatal("guest section state")
		}
		if !strings.Contains(strings.Join(m.listLines(60, 20), "\n"), "Необходим вход") {
			t.Fatal("guest hint")
		}
	}
}

func TestCatalogRefreshPreservesFullPlaylistAndSelection(t *testing.T) {
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			t.Errorf("catalog fetched tracks: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":{"generatedPlaylists":[{"ready":true,"data":{"kind":2,"title":"Updated","owner":{"uid":8},"tracks":[{"id":99,"track":{"id":99}}]}},{"ready":true,"data":{"kind":1,"owner":{"uid":8}}}]}}`))
	})
	m := newLive(context.Background(), a, &fakeAudio{})
	m.uid, m.section, m.busy = "7", music.SectionForYou, false
	var first, full music.Playlist
	if err := json.Unmarshal([]byte(`{"kind":1,"owner":{"uid":8}}`), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"kind":2,"revision":7,"title":"Full","owner":{"uid":8},"tracks":[{"id":10,"track":{"id":10}},{"id":11,"track":{"id":11}},{"id":12,"track":{"id":12}}]}`), &full); err != nil {
		t.Fatal(err)
	}
	m.remote = []music.Playlist{first, full}
	m.lists = []playlist{{}, {}}
	m.list = 1
	m.installPlaylist(full)
	m.selected, m.focus = 2, 2
	m.queue = []int{m.tracks()[1]}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'U'})
	n := next.(*liveModel)
	if cmd == nil {
		t.Fatal("U did not request catalog")
	}
	next, cmd = n.Update(cmd())
	n = next.(*liveModel)
	if cmd != nil || n.busy || n.list != 0 || n.remote[n.list].Kind != "2" || n.selected != 2 || n.focus != 2 {
		t.Fatal("refresh reset selection or automatically fetched playlist")
	}
	if len(n.tracks()) != 3 || len(n.remote[0].Tracks) != 3 || n.remote[0].Revision != 7 || n.lists[0].name != "Updated" || len(n.queue) != 1 {
		t.Fatal("preview replaced full playlist or queue")
	}
	preview := full
	preview.Kind = "3"
	next, cmd = n.Update(networkResult{serial: n.serial, kind: "catalog", data: []music.Playlist{preview}})
	n = next.(*liveModel)
	if cmd != nil || len(n.tracks()) != 0 || len(n.remote[0].Tracks) != 0 || n.remote[0].Revision != 0 || n.list != 0 || n.selected != 0 {
		t.Fatal("new catalog preview treated as complete")
	}
}

func TestExplicitPlaylistLoadKeysAndQueue(t *testing.T) {
	for _, key := range []rune{'l', 'L', 'д', 'Д'} {
		t.Run(string(key), func(t *testing.T) {
			a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/users/8/playlists/2":
					_, _ = w.Write([]byte(`{"result":{"kind":2,"owner":{"uid":8},"tracks":[{"id":1},{"id":2},{"id":3}]}}`))
				case "/tracks":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.Form.Get("track-ids") != "1,2,3" {
						t.Error("wrong track resolution request")
					}
					_, _ = w.Write([]byte(`{"result":[{"id":1,"title":"One"},{"id":2,"title":"Two"},{"id":3,"title":"Three"}]}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			})
			m := newLive(context.Background(), a, &fakeAudio{})
			m.busy = false
			p := music.Playlist{Kind: "2"}
			p.Owner.UID = "8"
			m.remote = []music.Playlist{{Kind: "1"}, p}
			m.lists = []playlist{{}, {}}
			m.list = 1
			m.queue = m.addTracks([]music.Track{{ID: "queued", Title: "Playing"}})
			m.playing, m.loaded, m.position = true, true, 42
			next, cmd := m.Update(tea.KeyPressMsg{Code: key})
			n := next.(*liveModel)
			if cmd == nil || !n.busy {
				t.Fatal("missing playlist command")
			}
			next, _ = n.Update(cmd())
			n = next.(*liveModel)
			if n.busy || n.list != 1 || len(n.tracks()) != 3 || n.library[n.tracks()[2]].title != "Three" {
				t.Fatal("not all returned tracks loaded into selected playlist")
			}
			if len(n.queue) != 1 || n.metadata[n.queue[0]].ID != "queued" || !n.playing || !n.loaded || n.position != 42 || n.current != 0 {
				t.Fatal("playlist load changed playback queue")
			}
		})
	}
}

func TestPlaylistLoadStaleErrorEmptyAndNavigation(t *testing.T) {
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	m.busy = false
	p := music.Playlist{Kind: "2", Title: "Full"}
	p.Owner.UID = "8"
	m.remote = []music.Playlist{p, {Kind: "3"}}
	m.lists = []playlist{{name: "Full", tracks: []int{0}}, {}}
	m.library = []track{{title: "Cached"}}
	m.metadata = []music.Track{{ID: "1"}}
	m.queue = []int{0}
	_ = m.openPlaylist()
	next, _ := m.Update(networkResult{serial: m.serial, kind: "playlist", err: errors.New("offline")})
	n := next.(*liveModel)
	if n.busy || len(n.tracks()) != 1 || n.notice != "offline" {
		t.Fatal("failed load lost cached tracks")
	}
	_ = n.openPlaylist()
	serial := n.serial
	next, _ = n.Update(networkResult{serial: serial - 1, kind: "playlist", data: p})
	n = next.(*liveModel)
	if !n.busy || len(n.tracks()) != 1 {
		t.Fatal("stale playlist accepted")
	}
	next, _ = n.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	n = next.(*liveModel)
	next, _ = n.Update(networkResult{serial: serial, kind: "playlist", data: p})
	n = next.(*liveModel)
	if n.list != 1 || n.focus != 0 || len(n.lists[0].tracks) != 0 || len(n.queue) != 1 {
		t.Fatal("empty result changed selection or queue")
	}
	n.list = 0
	next, _ = n.Update(networkResult{serial: serial, kind: "playlist", data: p})
	n = next.(*liveModel)
	if n.selected != 0 || len(n.tracks()) != 0 || !strings.Contains(strings.Join(n.trackLines(60, 20, false), " "), "L —") {
		t.Fatal("empty playlist not handled")
	}
	_ = n.openPlaylist()
	serial = n.serial
	_ = n.switchSection(music.SectionPersonal)
	next, _ = n.Update(networkResult{serial: serial, kind: "playlist", data: p})
	n = next.(*liveModel)
	if len(n.remote) != 0 || !n.busy {
		t.Fatal("stale playlist crossed sections")
	}
}

func TestPlaylistLoadEmptySearchAndBusy(t *testing.T) {
	a := testAPI(t, func(_ http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL.Path) })
	m := newLive(context.Background(), a, &fakeAudio{})
	m.busy = false
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'l'})
	n := next.(*liveModel)
	if cmd != nil || n.busy || !strings.Contains(n.notice, "Выберите") {
		t.Fatal("empty selection")
	}
	n.remote, n.lists = []music.Playlist{{Title: "Search"}}, []playlist{{name: "Search"}}
	next, cmd = n.Update(tea.KeyPressMsg{Code: 'l'})
	n = next.(*liveModel)
	if cmd != nil || n.busy || !strings.Contains(n.notice, "не плейлист") {
		t.Fatal("search sent to playlist API")
	}
	n.busy = true
	next, cmd = n.Update(tea.KeyPressMsg{Code: 'l'})
	if cmd != nil || !next.(*liveModel).busy {
		t.Fatal("busy request replaced")
	}
}

func TestPlaylistControlsVisible(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}} {
		m.width, m.height = size[0], size[1]
		for focus := range 3 {
			m.focus = focus
			view := m.View().Content
			if !strings.Contains(view, "U каталог") || !strings.Contains(view, "L треки") {
				t.Fatalf("controls missing at %v focus %d", size, focus)
			}
		}
	}
	m.width, m.height, m.dialog = 120, 40, "help"
	if !strings.Contains(m.View().Content, "L / Д") {
		t.Fatal("help missing playlist key")
	}
}

func TestGuestIntegration(t *testing.T) {
	if os.Getenv("YAMUSIC_INTEGRATION") != "1" {
		t.Skip("explicit guest network test")
	}
	a := music.New(music.Credentials{})
	ctx := context.Background()
	tracks, err := a.Search(ctx, "Nirvana", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) == 0 {
		t.Fatal("empty search")
	}
	lists, err := a.Chart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) == 0 {
		t.Fatal("empty chart")
	}
	p, err := a.Playlist(ctx, lists[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tracks) == 0 {
		t.Fatal("empty playlist")
	}
	info, err := a.Resolve(ctx, string(tracks[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Quality != "preview" {
		t.Fatalf("unexpected guest quality %s", info.Quality)
	}
	t.Logf("guest: search=%d, chart=%d, quality=%s", len(tracks), len(p.Tracks), info.Quality)
}
