package audio

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPlaybackEventLatch(t *testing.T) {
	for _, reason := range []string{"eof", "error", "stop", "quit", "redirect"} {
		t.Run(reason, func(t *testing.T) {
			a, b := net.Pipe()
			p := &MPV{eventConn: a, eventGeneration: 3}
			done := make(chan struct{})
			go func() { p.readEvents(a, 3); close(done) }()
			enc := json.NewEncoder(b)
			for _, v := range []map[string]string{{"event": "start-file"}, {"event": "file-loaded"}, {"event": "end-file", "reason": reason}} {
				if err := enc.Encode(v); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(time.Second)
			for !p.Events(3).Ready && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(time.Millisecond)
			state := p.Events(3)
			if state.Ended != (reason == "eof") || state.Failed != (reason == "error") {
				t.Fatal(state)
			}
			if p.Events(2) != (State{}) {
				t.Fatal("stale events")
			}
			_ = b.Close()
			<-done
		})
	}
}

func TestPlaybackMPVEOFSmoke(t *testing.T) {
	if os.Getenv("YAMUSIC_MPV_SMOKE") != "1" {
		t.Skip("optional synthetic mpv smoke")
	}
	root := t.TempDir()
	socket := filepath.Join(root, "ipc")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mpv", "--no-config", "--load-scripts=no", "--idle=yes", "--keep-open=no", "--no-video", "--ao=null", "--terminal=no", "--input-ipc-server="+socket)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &MPV{socket: socket, process: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	defer p.Close()
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		g := p.Advance()
		if _, err := p.Command(ctx, g, "stop"); err != nil {
			t.Fatal(err)
		}
		if err := p.Prepare(ctx, g); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Command(ctx, g, "loadfile", "av://lavfi:anullsrc=r=48000:cl=stereo:d=0.1", "replace"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(400 * time.Millisecond)
		s, err := Poll(ctx, p, g)
		if err != nil || !s.Ended || s.Failed {
			t.Fatal("missed EOF after idle", s, err)
		}
	}
}
