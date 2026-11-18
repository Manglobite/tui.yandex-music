package logfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"tui.yamusic/internal/music"
)

type Log struct {
	mu   sync.Mutex
	file *os.File
}
type playbackRecord struct {
	Time       string `json:"timestamp"`
	Stage      string `json:"stage"`
	Generation uint64 `json:"generation,omitempty"`
	Track      string `json:"track_id,omitempty"`
	State      string `json:"state,omitempty"`
	Error      string `json:"error,omitempty"`
	Duration   int64  `json:"duration_ms,omitempty"`
}

func Open(dir string) (*Log, error) {
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("playback-%s-%d-%d.log", time.Now().UTC().Format("20060102T150405.000000000"), os.Getpid(), i)
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, errors.New("не удалось создать playback-лог (проверьте права и свободное место)")
		}
		return &Log{file: f}, nil
	}
	return nil, errors.New("не удалось создать уникальный playback-лог")
}
func errorCategory(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ae music.Error
	if errors.As(err, &ae) {
		return "api_error"
	}
	return "operation_failed"
}
func safeTrack(id string) string {
	if len(id) > 80 {
		return "redacted"
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "redacted"
		}
	}
	return id
}
func (l *Log) Record(stage string, g uint64, id, state string, err error, elapsed time.Duration) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = json.NewEncoder(l.file).Encode(playbackRecord{time.Now().UTC().Format(time.RFC3339Nano), stage, g, safeTrack(id), state, errorCategory(err), elapsed.Milliseconds()})
	}
}
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}
