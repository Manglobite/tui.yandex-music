package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"
)

func (p *MPV) Prepare(ctx context.Context, g uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if g != p.Generation() {
		return context.Canceled
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", p.socket)
	if err != nil {
		return errors.New("mpv: event IPC недоступен")
	}
	if err = conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		_ = conn.Close()
		return errors.New("mpv: event IPC deadline")
	}
	if err = json.NewEncoder(conn).Encode(map[string]any{"command": []any{"get_property", "idle-active"}, "request_id": 1}); err != nil {
		_ = conn.Close()
		return errors.New("mpv: event IPC handshake")
	}
	reader := bufio.NewReader(conn)
	if _, err = reader.ReadBytes('\n'); err != nil {
		_ = conn.Close()
		return errors.New("mpv: event IPC handshake")
	}
	_ = conn.SetDeadline(time.Time{})
	p.eventMu.Lock()
	if p.eventConn != nil {
		_ = p.eventConn.Close()
	}
	p.eventConn = conn
	p.eventGeneration = g
	p.eventState = State{}
	p.eventMu.Unlock()
	go p.readEvents(conn, g)
	return nil
}
func (p *MPV) readEvents(conn net.Conn, g uint64) {
	defer func() { _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	started := false
	for scanner.Scan() {
		var event struct {
			Event  string `json:"event"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		p.eventMu.Lock()
		if p.eventConn != conn || p.eventGeneration != g {
			p.eventMu.Unlock()
			return
		}
		switch event.Event {
		case "start-file":
			started = true
		case "file-loaded":
			p.eventState.Ready = true
		case "end-file":
			if started {
				switch event.Reason {
				case "eof":
					p.eventState.Ended = true
				case "error":
					p.eventState.Failed = true
				}
			}
		}
		p.eventMu.Unlock()
	}
	p.eventMu.Lock()
	if p.eventConn == conn && p.eventGeneration == g {
		p.eventState.Failed = true
	}
	p.eventMu.Unlock()
}
func (p *MPV) Events(g uint64) State {
	p.eventMu.Lock()
	defer p.eventMu.Unlock()
	if g != p.eventGeneration {
		return State{}
	}
	return p.eventState
}
