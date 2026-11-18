package audio

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMPVIPC(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ipc")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close() }()
		var req struct {
			Command []any `json:"command"`
			ID      int   `json:"request_id"`
		}
		if err = json.NewDecoder(conn).Decode(&req); err != nil {
			done <- err
			return
		}
		encoder := json.NewEncoder(conn)
		if err = encoder.Encode(map[string]any{"event": "file-loaded"}); err != nil {
			done <- err
			return
		}
		done <- encoder.Encode(map[string]any{"request_id": req.ID, "error": "success", "data": 42})
	}()
	p := &MPV{socket: socket, done: make(chan struct{}), process: &exec.Cmd{}}
	raw, err := p.Command(context.Background(), 0, "get_property", "time-pos")
	if err != nil || string(raw) != "42" {
		t.Fatalf("response %s: %v", raw, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	p.Advance()
	if _, err = p.Command(context.Background(), 0, "stop"); err != context.Canceled {
		t.Fatalf("stale command: %v", err)
	}
}

func TestMissingMPV(t *testing.T) {
	p := &MPV{binary: "yamusic-nonexistent-player", root: t.TempDir()}
	defer p.Close()
	if _, err := p.Command(context.Background(), 0, "stop"); err == nil {
		t.Fatal("missing player accepted")
	}
}
