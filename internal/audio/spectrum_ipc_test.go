package audio

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSpectrumPropertyUnavailableIsPending(t *testing.T) {
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
			ID int `json:"request_id"`
		}
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			done <- err
			return
		}
		done <- json.NewEncoder(conn).Encode(map[string]any{"request_id": req.ID, "error": "property unavailable"})
	}()
	p := &MPV{socket: socket, done: make(chan struct{}), process: &exec.Cmd{}, spectrumInstalled: spectrumGroups}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := p.Spectrum(ctx, 0, true); !errors.Is(err, ErrSpectrumPending) {
		t.Fatal("expected pending spectrum metadata", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSpectrumIPCDeadline(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ipc")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buffer := make([]byte, 4096)
		_, _ = conn.Read(buffer)
		_, _ = conn.Read(buffer)
	}()
	p := &MPV{socket: socket, done: make(chan struct{}), process: &exec.Cmd{}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = p.Command(ctx, 0, "get_property", "af-metadata/spectrum"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing timeout", err)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("IPC timeout ignored")
	}
	<-done
	p.mu.Lock()
	ctx, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	_, err = p.Command(ctx, 0, "stop")
	p.mu.Unlock()
	if err == nil {
		t.Fatal("mutex wait ignored deadline")
	}
}
