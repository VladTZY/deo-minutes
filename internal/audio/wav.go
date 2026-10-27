// Package audio handles the 16 kHz mono signed 16-bit little-endian PCM the
// bot captures.
package audio

import (
	"encoding/binary"
	"io"
	"math"
	"os"
)

const (
	SampleRate     = 16000
	BytesPerSample = 2
	BytesPerSecond = SampleRate * BytesPerSample
)

// WAVWriter streams PCM into a WAV file and fixes the header on Close.
type WAVWriter struct {
	f    *os.File
	data uint32
}

func CreateWAV(path string) (*WAVWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	w := &WAVWriter{f: f}
	if err := w.header(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

func (w *WAVWriter) header() error {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+w.data)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], 1) // mono
	binary.LittleEndian.PutUint32(h[24:], SampleRate)
	binary.LittleEndian.PutUint32(h[28:], BytesPerSecond)
	binary.LittleEndian.PutUint16(h[32:], BytesPerSample)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], w.data)
	_, err := w.f.WriteAt(h, 0)
	return err
}

func (w *WAVWriter) Write(pcm []byte) (int, error) {
	n, err := w.f.WriteAt(pcm, int64(44+w.data))
	w.data += uint32(n)
	return n, err
}

func (w *WAVWriter) Close() error {
	if err := w.header(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}

// SilenceWAV writes a short silent WAV, used as the bot's fake microphone.
func SilenceWAV(path string, seconds int) error {
	w, err := CreateWAV(path)
	if err != nil {
		return err
	}
	if _, err := w.Write(make([]byte, seconds*BytesPerSecond)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// RMS returns the loudness of a PCM chunk in [0,1].
func RMS(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

// ReadPCM reads the data chunk of a WAV written by WAVWriter.
func ReadPCM(r io.ReadSeeker) ([]byte, error) {
	if _, err := r.Seek(44, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
