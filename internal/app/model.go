package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
)

// EventModelDownload carries ModelDownload progress to the frontend.
const EventModelDownload = "model_download"

// The speech model the app downloads when none is bundled: whisper.cpp's
// official Whisper large-v3-turbo q5_0 (MIT). Pinned by size and SHA-256.
// (vars so tests can point them at a local server)
var (
	modelURL          = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo-q5_0.bin"
	modelSHA256       = "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2"
	modelSize   int64 = 574041195
)

// ModelDownload is the progress of the model download.
type ModelDownload struct {
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Running  bool   `json:"running"`
	Finished bool   `json:"finished"`
	Error    string `json:"error"`
}

// DownloadModel fetches the speech model into the app data folder in the
// background. It resumes a partial download and verifies the checksum.
func (a *App) DownloadModel() error {
	a.mu.Lock()
	if a.downloading {
		a.mu.Unlock()
		return nil
	}
	a.downloading = true
	a.mu.Unlock()
	go func() {
		err := a.downloadModel(a.ctx, modelURL)
		a.mu.Lock()
		a.downloading = false
		a.mu.Unlock()
		p := ModelDownload{Total: modelSize, Finished: err == nil}
		if err != nil {
			slog.Error("model download", "err", err)
			p.Error = userMessage(err)
		} else {
			p.Done = modelSize
		}
		a.emitDownload(p)
	}()
	return nil
}

func (a *App) emitDownload(p ModelDownload) {
	if a.emit != nil {
		a.emit(EventModelDownload, p)
	}
}

func (a *App) downloadModel(ctx context.Context, url string) error {
	dir := config.ModelsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return i18n.Errorf(err, "err.model.download", err.Error())
	}
	final := filepath.Join(dir, modelFileName)
	part := final + ".partial"
	for attempt := 1; ; attempt++ {
		err := a.fetchPart(ctx, url, part)
		if err == nil {
			break
		}
		if ctx.Err() != nil || attempt == 20 {
			return i18n.Errorf(err, "err.model.download", "conexiune întreruptă")
		}
		slog.Warn("model download retry", "attempt", attempt, "err", err)
		time.Sleep(time.Duration(min(attempt, 6)) * 5 * time.Second)
	}
	if err := verifySHA256(part, modelSHA256); err != nil {
		_ = os.Remove(part)
		return i18n.Errorf(err, "err.model.download", "fișier corupt, încearcă din nou")
	}
	return os.Rename(part, final)
}

// fetchPart downloads into part, continuing from its current size.
func (a *App) fetchPart(ctx context.Context, url, part string) error {
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	have, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if have >= modelSize {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK: // server ignored the range: start over
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		have = 0
	default:
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	buf := make([]byte, 256<<10)
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			have += int64(n)
			if have > modelSize {
				return errors.New("download larger than expected")
			}
			if time.Since(last) > 500*time.Millisecond {
				last = time.Now()
				a.emitDownload(ModelDownload{Done: have, Total: modelSize, Running: true})
			}
		}
		if rerr == io.EOF {
			if have < modelSize {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("sha256 %s, want %s", got, want)
	}
	return nil
}
