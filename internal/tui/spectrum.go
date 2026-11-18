package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"tui.yamusic/internal/audio"
)

const spectrumInterval = 100 * time.Millisecond
const spectrumTimeout = 80 * time.Millisecond
const spectrumRetryInterval = time.Second
const spectrumUnsupported = "нет поддержки backend"

type spectrumAudio interface {
	Spectrum(context.Context, uint64, bool) ([audio.SpectrumBands]float64, error)
}

type spectrumTick struct{ epoch uint64 }
type spectrumResult struct {
	epoch, generation uint64
	levels            [audio.SpectrumBands]float64
	err               error
}

func (m *liveModel) resetSpectrum() {
	if m.spectrumCancel != nil {
		m.spectrumCancel()
		m.spectrumCancel = nil
	}
	m.spectrumEpoch++
	m.spectrumLevels = [audio.SpectrumBands]float64{}
	m.spectrumTimer = false
}

func (m *liveModel) scheduleSpectrum() tea.Cmd {
	if !m.spectrumOn || !m.loaded || !m.playing || m.playback == "BUFFERING" || m.spectrumTimer || m.spectrumBusy || m.spectrumHint == spectrumUnsupported {
		return nil
	}
	m.spectrumTimer = true
	epoch, ctx := m.spectrumEpoch, m.ctx
	interval := spectrumInterval
	if m.spectrumHint != "" {
		interval = spectrumRetryInterval
	}
	return func() tea.Msg {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return spectrumTick{epoch}
		}
	}
}

func (m *liveModel) readSpectrum() tea.Cmd {
	if !m.spectrumOn || !m.loaded || !m.playing || m.audioBusy || m.spectrumBusy {
		return m.scheduleSpectrum()
	}
	p, ok := m.audio.(spectrumAudio)
	if !ok {
		m.spectrumHint = spectrumUnsupported
		return nil
	}
	m.spectrumBusy = true
	epoch, g := m.spectrumEpoch, m.generation
	ctx, cancel := context.WithTimeout(m.ctx, spectrumTimeout)
	m.spectrumCancel = cancel
	return func() tea.Msg {
		defer cancel()
		levels, err := p.Spectrum(ctx, g, true)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, audio.ErrSpectrumPending) {
			cleanup, stop := context.WithTimeout(m.ctx, spectrumTimeout)
			defer stop()
			_, _ = p.Spectrum(cleanup, g, false)
		}
		return spectrumResult{epoch, g, levels, err}
	}
}

func (m *liveModel) disableSpectrum() tea.Cmd {
	m.resetSpectrum()
	p, ok := m.audio.(spectrumAudio)
	if !ok {
		return nil
	}
	ctx, g := m.ctx, m.generation
	return func() tea.Msg {
		cleanup, cancel := context.WithTimeout(ctx, spectrumTimeout)
		defer cancel()
		_, _ = p.Spectrum(cleanup, g, false)
		return nil
	}
}

func spectrumRows(levels [audio.SpectrumBands]float64, width int) []string {
	width = max(0, width)
	gap := 1
	if width < audio.SpectrumBands*2-1 {
		gap = 0
	}
	barWidth := max(1, min(12, (width-(audio.SpectrumBands-1)*gap)/audio.SpectrumBands))
	rows := make([]string, 3)
	const blocks = " ▁▂▃▄▅▆▇█"
	glyphs := []rune(blocks)
	for row := range rows {
		var b strings.Builder
		for i, level := range levels {
			if i > 0 && gap != 0 {
				b.WriteByte(' ')
			}
			n := int(math.Round(max(0, min(1, level))*24)) - (2-row)*8
			b.WriteString(strings.Repeat(string(glyphs[max(0, min(8, n))]), barWidth))
		}
		rows[row] = fit(b.String(), width)
	}
	return rows
}

func (m model) spectrumView() string {
	label := "BAND ENERGY · V вкл"
	if m.spectrumOn {
		label = "BAND ENERGY · V выкл"
	}
	if m.spectrumHint != "" {
		label = "BAND ENERGY · " + m.spectrumHint + " · V выкл"
	}
	rows := spectrumRows(m.spectrumLevels, min(m.width, max(audio.SpectrumBands, m.width-30)))
	if m.spectrumHint != "" {
		return strings.Join([]string{m.line(label, m.width), m.line(rows[1], m.width), m.line(rows[2], m.width)}, "\n")
	}
	rows[0] = fit(rows[0]+"  "+label, m.width)
	return strings.Join([]string{m.line(rows[0], m.width), m.line(rows[1], m.width), m.line(rows[2], m.width)}, "\n")
}
