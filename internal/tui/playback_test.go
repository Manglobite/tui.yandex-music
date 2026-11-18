package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"testing"
	"tui.yamusic/internal/audio"
	"tui.yamusic/internal/music"
)

func playbackModel() liveModel {
	m := newLive(context.Background(), music.New(music.Credentials{}), &fakeAudio{})
	m.busy = false
	m.queue = m.addTracks([]music.Track{{ID: "1"}, {ID: "2"}, {ID: "3"}})
	m.lists = []playlist{{tracks: []int{2, 0, 1}}, {tracks: []int{0}}}
	return m
}
func TestPlaybackRapidIntentAndCursor(t *testing.T) {
	m := playbackModel()
	for _, tc := range []struct {
		key               rune
		current, selected int
	}{{'n', 1, 2}, {'n', 2, 0}, {'p', 1, 2}} {
		next, cmd := m.Update(tea.KeyPressMsg{Code: tc.key})
		m = *next.(*liveModel)
		if cmd == nil || m.current != tc.current || m.queueSelected != tc.current || m.selected != tc.selected {
			t.Fatal("lost intent or cursor", tc, m.current, m.selected)
		}
	}
	g := m.generation
	for _, kind := range []string{"poll", "load", "command"} {
		next, _ := m.Update(audioResult{generation: g - 1, kind: kind, state: audio.State{Ended: true}, err: errors.New("stale")})
		m = *next.(*liveModel)
		if !m.audioBusy || m.generation != g || m.current != 1 {
			t.Fatal("stale changed load")
		}
	}
	m.list = 1
	_ = m.startTrack(2)
	if m.selected != 2 {
		t.Fatal("foreign playlist selection changed")
	}
	m.selected = 0
	next, _ := m.Update(audioResult{generation: m.generation, kind: "load"})
	m = *next.(*liveModel)
	next, _ = m.Update(audioResult{generation: m.generation, kind: "poll", state: audio.State{Position: 5}})
	m = *next.(*liveModel)
	if m.selected != 0 {
		t.Fatal("tick stole manual navigation")
	}
}
func TestPlaybackIdleEOFAndPollRevision(t *testing.T) {
	m := playbackModel()
	_ = m.startTrack(0)
	next, _ := m.Update(audioResult{generation: m.generation, kind: "load"})
	m = *next.(*liveModel)
	next, _ = m.Update(audioResult{generation: m.generation, kind: "poll", state: audio.State{Idle: true}})
	m = *next.(*liveModel)
	if !m.loaded {
		t.Fatal("load transition became stop")
	}
	m.audioRevision++
	next, _ = m.Update(audioResult{generation: m.generation, revision: 0, kind: "poll", state: audio.State{Ended: true}})
	m = *next.(*liveModel)
	if m.current != 0 {
		t.Fatal("stale poll advanced")
	}
	next, cmd := m.Update(audioResult{generation: m.generation, revision: m.audioRevision, kind: "poll", state: audio.State{Idle: true, Ended: true}})
	m = *next.(*liveModel)
	if cmd == nil || m.current != 1 || m.queueSelected != 1 || m.selected != 2 {
		t.Fatal("idle EOF did not advance")
	}
	next, _ = m.Update(audioResult{generation: m.generation - 1, kind: "poll", state: audio.State{Ended: true}})
	if next.(*liveModel).current != 1 {
		t.Fatal("double EOF")
	}
}
func TestPlaybackPollDoesNotBlockCommand(t *testing.T) {
	m := playbackModel()
	m.loaded, m.playing, m.pollBusy = true, true, true
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = *next.(*liveModel)
	if cmd == nil || !m.audioBusy {
		t.Fatal("poll swallowed pause")
	}
	next, _ = m.Update(audioResult{kind: "poll", revision: 0})
	if !next.(*liveModel).audioBusy {
		t.Fatal("poll unlocked command")
	}
}
func TestPlaybackSpectrumTimeoutNonfatal(t *testing.T) {
	m := playbackModel()
	m.loaded, m.playing, m.spectrumOn = true, true, true
	next, _ := m.Update(spectrumResult{err: context.DeadlineExceeded})
	n := next.(*liveModel)
	if !n.loaded || !n.playing || !n.spectrumOn {
		t.Fatal("contention disabled playback or spectrum")
	}
}
