package ui

import (
	"bytes"
	"runtime"
	"sync"
	"testing"
)

// pieceWriter copies p in small pieces, yielding between them, the way a large
// write to a busy terminal can be split. Without a lock, concurrent writers
// interleave their pieces.
type pieceWriter struct{ buf bytes.Buffer }

func (w *pieceWriter) Write(p []byte) (int, error) {
	for i := 0; i < len(p); i += 64 {
		w.buf.Write(p[i:min(i+64, len(p))])
		runtime.Gosched()
	}
	return len(p), nil
}

// Frames and image uploads are written from different goroutines. Each write
// must reach the terminal whole, or an upload's escape sequence is cut by a
// frame and its base64 shows up as text.
func TestWriteLockedKeepsWritesWhole(t *testing.T) {
	var out pieceWriter
	const size = 8192
	var wg sync.WaitGroup
	for _, c := range []byte("abcdefgh") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = writeLocked(&out, bytes.Repeat([]byte{c}, size))
		}()
	}
	wg.Wait()

	b := out.buf.Bytes()
	if len(b) != 8*size {
		t.Fatalf("got %d bytes, want %d", len(b), 8*size)
	}
	for i := 0; i < len(b); i += size {
		if len(bytes.Trim(b[i:i+size], string(b[i]))) != 0 {
			t.Fatalf("write starting at %d was interleaved with another", i)
		}
	}
}

func TestSerializedStdoutIsAFile(t *testing.T) {
	if _, ok := SerializedStdout().(interface{ Fd() uintptr }); !ok {
		t.Error("Bubble Tea needs Fd() to detect the terminal and its size")
	}
}
