package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStaleCommandReply(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ipc")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		close(entered)
		<-release
		_ = json.NewEncoder(conn).Encode(map[string]any{"request_id": 1, "error": "success", "data": false})
	}()
	p := &MPV{socket: socket, process: &exec.Cmd{}, done: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, e := p.Command(context.Background(), 0, "stop"); result <- e }()
	<-entered
	p.Advance()
	close(release)
	if e := <-result; e != context.Canceled {
		t.Fatal("stale reply accepted", e)
	}
	<-done
}
