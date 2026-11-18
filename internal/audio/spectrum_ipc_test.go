package audio

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

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
