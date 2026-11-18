package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"tui.yamusic/internal/audio"
	"tui.yamusic/internal/music"
)

type meterAudio struct {
	fakeAudio
	reads, removes int
	err            error
}

func (p *meterAudio) Spectrum(_ context.Context, _ uint64, on bool) ([audio.SpectrumBands]float64, error) {
	if on {
		p.reads++
	} else {
		p.removes++
	}
	return [audio.SpectrumBands]float64{1, .8, .6, .4, .2, 0}, p.err
}

func TestSpectrumLifecycle(t *testing.T) {
	p := &meterAudio{}
	m := newLive(context.Background(), music.New(music.Credentials{}), p)
	m.loaded, m.playing, m.spectrumOn = true, true, true
	m.generation = 2
	cmd := m.readSpectrum()
	if cmd == nil || !m.spectrumBusy || m.readSpectrum() != nil {
		t.Fatal("single flight")
	}
	result := cmd().(spectrumResult)
	m.resetSpectrum()
	next, _ := m.Update(result)
	m = *next.(*liveModel)
	if m.spectrumLevels[0] != 0 || m.spectrumBusy {
		t.Fatal("stale epoch")
	}
	m.spectrumTimer = false
	cmd = m.readSpectrum()
	next, _ = m.Update(cmd())
	m = *next.(*liveModel)
	if m.spectrumLevels[0] != 1 {
		t.Fatal("no levels")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = *next.(*liveModel)
	if m.spectrumOn || m.spectrumLevels[0] != 0 || m.readSpectrum() != nil {
		t.Fatal("off")
	}
	if cmd != nil {
		cmd()
	}
	if p.removes != 1 {
		t.Fatal("filter retained")
	}
	m.spectrumOn, m.playing = true, false
	if m.readSpectrum() != nil {
		t.Fatal("pause polls")
	}
	m.playing = true
	m.spectrumTimer = false
	cmd = m.readSpectrum()
	result = cmd().(spectrumResult)
	result.generation = 1
	next, _ = m.Update(result)
	m = *next.(*liveModel)
	if m.spectrumLevels[0] != 0 {
		t.Fatal("stale generation")
	}
	m.spectrumTimer = false
	cmd = m.readSpectrum()
	result = cmd().(spectrumResult)
	result.err = fmt.Errorf("missing lavfi")
	next, cmd = m.Update(result)
	m = *next.(*liveModel)
	if m.spectrumOn || !m.playing || !m.loaded || m.spectrumHint == "" {
		t.Fatal("not fail open")
	}
	if cmd != nil {
		cmd()
	}
	if m.readSpectrum() != nil {
		t.Fatal("error polls")
	}
}

func TestSpectrumPauseStopError(t *testing.T) {
	for _, action := range []string{"pause", "stop", "error"} {
		p := &meterAudio{}
		m := newLive(context.Background(), music.New(music.Credentials{}), p)
		m.loaded, m.playing, m.spectrumOn = true, true, true
		m.spectrumLevels[0] = 1
		var msg tea.Msg
		switch action {
		case "pause":
			msg = tea.KeyPressMsg{Code: tea.KeySpace}
		case "stop":
			msg = tea.KeyPressMsg{Code: 's', Text: "s"}
		case "error":
			m.pollErrors = 4
			msg = audioResult{kind: "poll", err: fmt.Errorf("audio failed")}
		}
		next, _ := m.Update(msg)
		n := next.(*liveModel)
		if n.spectrumLevels[0] != 0 || n.playing || n.scheduleSpectrum() != nil {
			t.Fatal(action)
		}
	}
}

func TestSpectrumPendingMetadata(t *testing.T) {
	p := &meterAudio{err: audio.ErrSpectrumPending}
	m := newLive(context.Background(), music.New(music.Credentials{}), p)
	m.loaded, m.playing = true, true
	m.playback = "BUFFERING"
	if m.scheduleSpectrum() != nil {
		t.Fatal("polling before playback ready")
	}
	m.playback = "PLAYING"
	cmd := m.readSpectrum()
	if cmd == nil {
		t.Fatal("missing read")
	}
	next, retry := m.Update(cmd())
	m = *next.(*liveModel)
	if !m.spectrumOn || retry == nil || p.removes != 0 || m.spectrumHint != "" {
		t.Fatal("pending metadata disabled visualization")
	}
}

func TestSpectrumDefaultAndHeader(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := newLive(context.Background(), music.New(music.Credentials{}), &meterAudio{})
	if !m.spectrumOn {
		t.Fatal("spectrum disabled by default")
	}
	m.account = true
	m.width, m.height = 120, 40
	header := strings.Split(m.View().Content, "\n")[0]
	if strings.TrimSpace(ansi.Strip(header)) != "Y A M U S I C  // YANDEX ACCOUNT / A выход" {
		t.Fatal(header)
	}
	for i := range m.spectrumLevels {
		m.spectrumLevels[i] = 1
	}
	for _, width := range []int{60, 80, 120} {
		m.width = width
		row := strings.Split(m.spectrumView(), "\n")[1]
		if strings.Count(row, "█") != 36 {
			t.Fatal("missing bars", width, row)
		}
	}
}

func TestSpectrumRows(t *testing.T) {

	var levels [audio.SpectrumBands]float64
	for i := range levels {
		levels[i] = 1
	}
	for _, width := range []int{0, 1, 17, 18, 30, 35, 36, 50, 70, 71, 90, 120} {
		rows := spectrumRows(levels, width)
		if len(rows) != 3 {
			t.Fatal("block height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatal("renderer overflow", width)
			}
			if width >= audio.SpectrumBands*2-1 && len(strings.Fields(row)) != audio.SpectrumBands {
				t.Fatal("missing columns", width, row)
			}
			if width >= audio.SpectrumBands && width < audio.SpectrumBands*2-1 && strings.Count(row, "█") != audio.SpectrumBands {
				t.Fatal("compact columns", width, row)
			}
		}
	}
}

func TestSpectrumLayout(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	{
		for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {1, 1}} {
			m := newModel()
			m.live, m.spectrumOn = true, true
			m.width, m.height = size[0], size[1]
			m.spectrumLevels = [audio.SpectrumBands]float64{0, .2, .4, .6, .8, 1}
			text := m.View().Content
			block := ansi.Strip(m.spectrumView())
			for _, frequency := range []string{"80", "250", "750", "2k", "6k", "12k", "Hz"} {
				if strings.Contains(block, frequency) {
					t.Fatal("frequency label", frequency, block)
				}
			}
			if m.width >= 60 && !strings.Contains(block, "V выкл") {
				t.Fatal("toggle missing", block)
			}
			rows := strings.Split(text, "\n")
			if len(rows) > m.height {
				t.Fatal("height", size)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > m.width {
					t.Fatal("width", size)
				}
			}
			if m.width >= 60 && (!strings.Contains(text, "BAND ENERGY") || !strings.Contains(text, "Q выход")) {
				t.Fatal("missing block/footer", size, text)
			}
		}
	}
}

func BenchmarkSpectrumRenderer(b *testing.B) {
	for _, width := range []int{120, 80} {
		b.Run(fmt.Sprint(width), func(b *testing.B) {
			levels := [audio.SpectrumBands]float64{.1, .3, .5, .7, .9, 1}
			b.ReportAllocs()
			for b.Loop() {
				_ = spectrumRows(levels, width)
			}
		})
	}
}
func BenchmarkViewSpectrum(b *testing.B) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		for _, animated := range []bool{false, true} {
			b.Run(fmt.Sprintf("%dx%d/animated=%t", size[0], size[1], animated), func(b *testing.B) {
				m := newModel()
				m.width, m.height = size[0], size[1]
				m.live = true
				m.spectrumOn = animated
				b.ReportAllocs()
				frame := 0
				for b.Loop() {
					if animated {
						frame++
						m.spectrumLevels[0] = float64(frame%24) / 24
					}
					_ = m.view(animated)
				}
			})
		}
	}
}
