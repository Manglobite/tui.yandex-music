package audio

import "sync"

type mpvDiagnostics struct {
	mu    sync.Mutex
	bytes int
}

func (d *mpvDiagnostics) Write(b []byte) (int, error) {
	d.mu.Lock()
	d.bytes = min(65536, d.bytes+len(b))
	d.mu.Unlock()
	return len(b), nil
}
func (d *mpvDiagnostics) size() int { d.mu.Lock(); defer d.mu.Unlock(); return d.bytes }
