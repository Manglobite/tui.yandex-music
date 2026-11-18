package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"tui.yamusic/internal/logfile"
)

type Backend interface {
	Generation() uint64
	Advance() uint64
	Command(context.Context, uint64, ...any) (json.RawMessage, error)
	Close()
}
type MPV struct {
	eventMu                   sync.Mutex
	eventConn                 net.Conn
	eventGeneration           uint64
	eventState                State
	log                       *logfile.Log
	spectrumMu                sync.Mutex
	spectrumInstalled         int
	mu                        sync.Mutex
	generation                atomic.Uint64
	binary, root, dir, socket string
	process                   *exec.Cmd
	done                      chan struct{}
	closed                    bool
}

func New(binary, root string, log *logfile.Log) *MPV {
	return &MPV{binary: binary, root: root, log: log}
}

func (p *MPV) Generation() uint64 { return p.generation.Load() }
func (p *MPV) Advance() uint64    { return p.generation.Add(1) }
func (p *MPV) start(ctx context.Context) error {
	if p.closed {
		return errors.New("плеер закрыт")
	}
	if p.process != nil {
		select {
		case <-p.done:
			p.process = nil
		default:
			return nil
		}
	}
	binary, err := exec.LookPath(p.binary)
	if err != nil {
		return errors.New("mpv не найден: установите mpv для воспроизведения")
	}
	if p.dir != "" {
		if err = os.RemoveAll(p.dir); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(p.root, 0700); err != nil {
		return err
	}
	p.dir, err = os.MkdirTemp(p.root, "mpv-")
	if err != nil {
		return err
	}
	p.socket = filepath.Join(p.dir, "ipc")
	if len(p.socket) > 100 {
		return errors.New("слишком длинный путь проекта для Unix socket")
	}
	cmd := exec.Command(binary, "--idle=yes", "--no-video", "--audio-display=no", "--force-window=no", "--no-config", "--load-scripts=no", "--terminal=no", "--keep-open=no", "--network-timeout=15", "--input-ipc-server="+p.socket)
	diagnostics := &mpvDiagnostics{}
	cmd.Stderr = diagnostics
	if err = cmd.Start(); err != nil {
		return errors.New("не удалось запустить mpv")
	}
	p.process = cmd
	p.done = make(chan struct{})
	done := p.done
	p.log.Record("mpv_start", p.Generation(), "", "", nil, 0)
	go func() {
		err := cmd.Wait()
		state := "quiet"
		if diagnostics.size() > 0 {
			state = "stderr_redacted"
		}
		p.log.Record("mpv_exit", p.Generation(), "", state, err, 0)
		close(done)
	}()
	ready := false
	defer func() {
		if !ready {
			_ = cmd.Process.Kill()
			<-done
			p.process = nil
		}
	}()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("mpv завершился при запуске")
		case <-timer.C:
			return errors.New("mpv: IPC не появился")
		case <-tick.C:
			if _, err = os.Stat(p.socket); err == nil {
				ready = true
				return nil
			}
		}
	}
}
func (p *MPV) Command(ctx context.Context, generation uint64, args ...any) (json.RawMessage, error) {
	for !p.mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	defer p.mu.Unlock()
	if generation != p.Generation() {
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.start(ctx); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", p.socket)
	if err != nil {
		return nil, errors.New("mpv: IPC недоступен")
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if err = json.NewEncoder(conn).Encode(map[string]any{"command": args, "request_id": 1}); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("mpv: ошибка отправки")
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var response struct {
			ID    int             `json:"request_id"`
			Error string          `json:"error"`
			Data  json.RawMessage `json:"data"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil {
			return nil, errors.New("mpv: некорректный JSON")
		}
		if response.ID != 1 {
			continue
		}
		if response.Error != "success" {
			return nil, fmt.Errorf("mpv: команда %v не выполнена", args[0])
		}
		if generation != p.Generation() {
			return nil, context.Canceled
		}
		return response.Data, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return nil, context.DeadlineExceeded
		}
	}
	return nil, errors.New("mpv: соединение закрыто")
}
func (p *MPV) Close() {
	p.Advance()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.eventMu.Lock()
	if p.eventConn != nil {
		_ = p.eventConn.Close()
		p.eventConn = nil
	}
	p.eventMu.Unlock()
	if p.process != nil {
		_ = p.process.Process.Kill()
		<-p.done
	}
	if p.dir != "" {
		_ = os.RemoveAll(p.dir)
	}
}

type State struct {
	Position, Duration             float64
	Paused, Ended, Idle, Buffering bool
	Ready, Failed                  bool
}

func Poll(ctx context.Context, p Backend, generation uint64) (State, error) {
	var s State
	if events, ok := p.(interface{ Events(uint64) State }); ok {
		s = events.Events(generation)
		if s.Ended || s.Failed {
			return s, nil
		}
	}
	fields := []struct {
		name string
		out  any
	}{{"idle-active", &s.Idle}, {"pause", &s.Paused}, {"eof-reached", &s.Ended}}
	for _, field := range fields {
		if s.Idle {
			return s, nil
		}
		data, err := p.Command(ctx, generation, "get_property", field.name)
		if err != nil {
			return s, err
		}
		if err = json.Unmarshal(data, field.out); err != nil {
			return s, err
		}
	}
	if s.Idle || s.Ended {
		return s, nil
	}
	for _, field := range []struct {
		name string
		out  any
	}{{"time-pos", &s.Position}, {"duration", &s.Duration}, {"paused-for-cache", &s.Buffering}} {
		if s.Idle {
			return s, nil
		}
		data, err := p.Command(ctx, generation, "get_property", field.name)
		if err != nil {
			return s, err
		}
		if err = json.Unmarshal(data, field.out); err != nil {
			return s, err
		}
	}
	return s, nil
}
