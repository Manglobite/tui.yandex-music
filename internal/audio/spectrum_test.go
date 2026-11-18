package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spectrumMetadata(db string) json.RawMessage {
	values := map[string]string{}
	for i := 3; i <= 14; i++ {
		values[fmt.Sprintf("lavfi.astats.%d.RMS_level", i)] = db
	}
	raw, _ := json.Marshal(values)
	return raw
}

func TestSpectrumParsing(t *testing.T) {
	for _, tc := range []struct {
		db   string
		want float64
		bad  bool
	}{
		{"-inf", 0, false}, {"-60", 0, false}, {"-30", .5, false}, {"0", 1, false}, {"12", 1, false},
		{"NaN", 0, true}, {"inf", 0, true}, {"broken", 0, true},
	} {
		got, err := parseSpectrum(spectrumMetadata(tc.db))
		if (err != nil) != tc.bad {
			t.Fatalf("%s: %v %v", tc.db, got, err)
		}
		if err == nil {
			for _, level := range got {
				if math.Abs(level-tc.want) > 1e-8 {
					t.Fatalf("%s: %v", tc.db, got)
				}
			}
		}
	}
	for _, raw := range []string{`null`, `{}`, `false`, `{"lavfi.astats.3.RMS_level":3}`} {
		if _, err := parseSpectrum(json.RawMessage(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	raw := strings.Replace(string(spectrumMetadata("-inf")), `"lavfi.astats.3.RMS_level":"-inf"`, `"lavfi.astats.3.RMS_level":"-20"`, 1)
	levels, err := parseSpectrum(json.RawMessage(raw))
	if err != nil || math.Abs(levels[0]-(40-10*math.Log10(2))/60) > 1e-8 {
		t.Fatalf("power combine: %v %v", levels, err)
	}
}

func TestSpectrumBands(t *testing.T) {
	if SpectrumBands != 36 || spectrumGroups != 6 {
		t.Fatal("band count")
	}
	ratio := math.Pow(12000.0/80, 1.0/35)
	for band := range SpectrumBands {
		f := spectrumFrequency(band)
		if band > 0 && math.Abs(f/spectrumFrequency(band-1)-ratio) > 1e-10 {
			t.Fatal("non-logarithmic spacing", band)
		}
		if !strings.Contains(spectrumGraph(band/spectrumGroupBands), fmt.Sprintf("bandpass=f=%.9f:", f)) {
			t.Fatal("missing independent filter", band)
		}
	}
	if math.Abs(spectrumFrequency(0)-80) > 1e-8 || math.Abs(spectrumFrequency(35)-12000) > 1e-8 {
		t.Fatal("frequency range")
	}
}

func TestSpectrumMPVSmoke(t *testing.T) {
	if os.Getenv("YAMUSIC_MPV_SMOKE") != "1" {
		t.Skip("set YAMUSIC_MPV_SMOKE=1 for local synthetic mpv")
	}
	root := t.TempDir()
	socket := filepath.Join(root, "ipc")
	cmd := exec.Command("mpv", "--no-config", "--load-scripts=no", "--idle=yes", "--no-video", "--ao=null", "--terminal=no", "--input-ipc-server="+socket)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &MPV{socket: socket, process: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	for _, source := range []string{"aevalsrc=0.2*sin(2*PI*250*t)|-0.2*sin(2*PI*250*t):s=48000:d=0.2:c=stereo", "aevalsrc=0.2*sin(2*PI*80*t)+0.1*sin(2*PI*6000*t)|0.3*sin(2*PI*12000*t):s=48000:d=0.2:c=stereo", "anullsrc=r=48000:cl=stereo:d=0.2"} {
		render := func(filter string) []byte {
			args := []string{"-v", "error", "-f", "lavfi", "-i", source}
			if filter != "" {
				args = append(args, "-af", filter)
			}
			args = append(args, "-f", "f32le", "-")
			data, err := exec.CommandContext(ctx, "ffmpeg", args...).Output()
			if err != nil {
				t.Fatal("PCM graph", err)
			}
			return data
		}
		graphs := make([]string, spectrumGroups)
		for group := range graphs {
			graphs[group] = spectrumGraph(group)
		}
		original, filtered := render(""), render(strings.Join(graphs, ","))
		if !bytes.Equal(original, filtered) {
			t.Fatalf("PCM changed: %d vs %d bytes", len(original), len(filtered))
		}
	}
	for sample := range SpectrumBands + 2 {
		want := sample % SpectrumBands
		frequency := spectrumFrequency(want)
		left := fmt.Sprintf("0.2*sin(2*PI*%.9f*t)", frequency)
		right := "-" + left
		if sample == SpectrumBands {
			right = "0"
		}
		if sample == SpectrumBands+1 {
			left = "0"
		}
		source := fmt.Sprintf("av://lavfi:aevalsrc=%s|%s:s=48000:d=3:c=stereo", left, right)
		if _, err := p.Command(ctx, 0, "loadfile", source, "replace"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond)
		if _, err := p.Spectrum(ctx, 0, true); err != nil {
			t.Fatal("attach", err)
		}
		time.Sleep(250 * time.Millisecond)
		levels, err := p.Spectrum(ctx, 0, true)
		if err != nil {
			t.Fatal("metadata", err)
		}
		peak := 0
		for i, v := range levels {
			if v > levels[peak] {
				peak = i
			}
		}
		if peak != want || levels[peak] < .5 {
			t.Fatalf("band %d (%.3f Hz), peak %d: %v", want, frequency, peak, levels)
		}
		raw, err := p.Command(ctx, 0, "get_property", "audio-params/channel-count")
		if err != nil || string(raw) != "2" {
			t.Fatalf("stereo: %s %v", raw, err)
		}
		if _, err = p.Spectrum(ctx, 0, false); err != nil {
			t.Fatal("remove", err)
		}
	}
	if _, err := p.Command(ctx, 0, "loadfile", "av://lavfi:anullsrc=r=48000:cl=stereo:d=2", "replace"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := p.Spectrum(ctx, 0, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if levels, err := p.Spectrum(ctx, 0, true); err != nil || levels != [SpectrumBands]float64{} {
		t.Fatalf("silence: %v %v", levels, err)
	}
	if _, err := p.Spectrum(ctx, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Command(ctx, 0, "af", "add", "@spectrum:lavfi=[not_a_filter]"); err == nil {
		t.Fatal("invalid graph accepted")
	}
	if _, err := p.Command(ctx, 0, "get_property", "time-pos"); err != nil {
		t.Fatal("fail-open playback", err)
	}
}
