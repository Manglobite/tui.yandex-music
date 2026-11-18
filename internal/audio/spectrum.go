package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var ErrSpectrumPending = errors.New("RMS metadata ещё не готовы")

const SpectrumBands = 36
const spectrumGroupBands = 6
const spectrumGroups = SpectrumBands / spectrumGroupBands

func spectrumFrequency(band int) float64 {
	return 80 * math.Pow(12000.0/80, float64(band)/float64(SpectrumBands-1))
}

func spectrumName(group int) string {
	return fmt.Sprintf("spectrum%d", group)
}

func spectrumGraph(group int) string {
	var b strings.Builder
	b.WriteString("aformat=sample_fmts=fltp:sample_rates=48000:channel_layouts=stereo,asplit=7[main][b0][b1][b2][b3][b4][b5];")
	for i := range spectrumGroupBands {
		f := spectrumFrequency(group*spectrumGroupBands + i)
		fmt.Fprintf(&b, "[b%d]bandpass=f=%.9f:t=o:w=%.9f,aformat=channel_layouts=stereo[x%d];", i, f, math.Log2(12000.0/80)/float64(SpectrumBands-1), i)
	}
	b.WriteString("[main][x0][x1][x2][x3][x4][x5]amerge=inputs=7,asetnsamples=n=2048:p=0,astats=metadata=1:reset=1:measure_perchannel=RMS_level:measure_overall=none,pan=stereo|c0=c0|c1=c1")
	return b.String()
}

func parseSpectrum(raw json.RawMessage) ([spectrumGroupBands]float64, error) {
	var levels [spectrumGroupBands]float64
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return levels, err
	}
	for i := range levels {
		power := 0.0
		for channel := 3 + i*2; channel <= 4+i*2; channel++ {
			value, ok := metadata[fmt.Sprintf("lavfi.astats.%d.RMS_level", channel)]
			if !ok {
				return levels, ErrSpectrumPending
			}
			db, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(db) || math.IsInf(db, 1) {
				return levels, errors.New("неверный RMS")
			}
			power += math.Pow(10, db/10) / 2
		}
		if power > 0 {
			levels[i] = max(0, min(1, (10*math.Log10(power)+60)/60))
		}
	}
	return levels, nil
}

func (p *MPV) Spectrum(ctx context.Context, generation uint64, enabled bool) ([SpectrumBands]float64, error) {
	var zero [SpectrumBands]float64
	p.spectrumMu.Lock()
	defer p.spectrumMu.Unlock()
	if !enabled {
		for p.spectrumInstalled > 0 {
			group := p.spectrumInstalled - 1
			if _, err := p.Command(ctx, generation, "af", "remove", "@"+spectrumName(group)); err != nil {
				return zero, err
			}
			p.spectrumInstalled--
		}
		return zero, nil
	}
	if p.spectrumInstalled < spectrumGroups {
		for p.spectrumInstalled < spectrumGroups {
			group := p.spectrumInstalled
			if _, err := p.Command(ctx, generation, "af", "add", "@"+spectrumName(group)+":lavfi=["+spectrumGraph(group)+"]"); err != nil {
				return zero, err
			}
			p.spectrumInstalled++
		}
		return zero, nil
	}
	var levels [SpectrumBands]float64
	for group := range spectrumGroups {
		raw, err := p.Command(ctx, generation, "get_property", "af-metadata/"+spectrumName(group))
		if errors.Is(err, errPropertyUnavailable) {
			return zero, ErrSpectrumPending
		}
		if err != nil {
			return zero, err
		}
		part, err := parseSpectrum(raw)
		if err != nil {
			return zero, err
		}
		copy(levels[group*spectrumGroupBands:], part[:])
	}
	return levels, nil
}
