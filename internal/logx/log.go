// Package logx writes JSON logs to %LOCALAPPDATA%\DEOMinutes\logs with
// size-based rotation and redaction of secrets, URLs and transcript text.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxBytes = 5 << 20
	keep     = 5
	fileName = "deominute.log"
)

type rotatingFile struct {
	mu   sync.Mutex
	dir  string
	f    *os.File
	size int64
}

func openRotating(dir string) (*rotatingFile, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	r := &rotatingFile{dir: dir}
	return r, r.open()
}

func (r *rotatingFile) path(i int) string {
	if i == 0 {
		return filepath.Join(r.dir, fileName)
	}
	return filepath.Join(r.dir, fmt.Sprintf("%s.%d", fileName, i))
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path(0), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotatingFile) rotate() error {
	_ = r.f.Close()
	_ = os.Remove(r.path(keep))
	for i := keep - 1; i >= 0; i-- {
		_ = os.Rename(r.path(i), r.path(i+1))
	}
	return r.open()
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > maxBytes {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// New returns a redacting JSON logger writing to w.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceAttr}))
}

// Setup installs the file logger as slog's default and returns a closer.
func Setup(dir string) (io.Closer, error) {
	rf, err := openRotating(dir)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(New(rf, slog.LevelInfo))
	return rf, nil
}
