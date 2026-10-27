package audio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWAVRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.wav")
	w, err := CreateWAV(p)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, BytesPerSecond)
	for i := 0; i < SampleRate; i++ {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(i%2000-1000)))
	}
	if _, err := w.Write(pcm); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b[0:4]) != "RIFF" || binary.LittleEndian.Uint32(b[40:]) != BytesPerSecond || len(b) != 44+BytesPerSecond {
		t.Fatalf("bad header/size: %d", len(b))
	}
	f, _ := os.Open(p)
	defer func() { _ = f.Close() }()
	got, err := ReadPCM(f)
	if err != nil || len(got) != len(pcm) {
		t.Fatalf("read %d %v", len(got), err)
	}
}

func TestRMS(t *testing.T) {
	if RMS(make([]byte, 100)) != 0 {
		t.Fatal("silence must be 0")
	}
	loud := make([]byte, 4)
	pos, neg := int16(16384), int16(-16384)
	binary.LittleEndian.PutUint16(loud, uint16(pos))
	binary.LittleEndian.PutUint16(loud[2:], uint16(neg))
	if r := RMS(loud); r < 0.49 || r > 0.51 {
		t.Fatalf("rms %f", r)
	}
}
