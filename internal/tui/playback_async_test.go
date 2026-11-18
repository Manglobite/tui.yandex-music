package tui

import (
	"net/http"
	"testing"
	"time"
)

func TestDelayedResolveLastIntent(t *testing.T) {
	entered := make(chan struct{})
	a := testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("trackId") == "2" {
			close(entered)
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`{"result":{"downloadInfo":{"url":"https://example.test/audio","transport":"raw"}}}`))
	})
	m := playbackModel()
	m.api = a
	old := m.startTrack(1)
	result := make(chan any, 1)
	go func() { result <- old() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("resolve did not start")
	}
	latest := m.startTrack(2)
	next, _ := m.Update(<-result)
	m = *next.(*liveModel)
	if m.current != 2 || !m.audioBusy {
		t.Fatal("stale resolve changed intent")
	}
	next, _ = m.Update(latest())
	m = *next.(*liveModel)
	if !m.loaded || m.current != 2 {
		t.Fatal("latest intent not loaded", m.notice)
	}
}
