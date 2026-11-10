//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package termimage

import (
	"os"
	"testing"
	"time"
)

func TestReadReady(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if ok, err := readReady(int(r.Fd()), 10*time.Millisecond); err != nil || ok {
		t.Fatalf("empty pipe: ready=%v err=%v, want false nil", ok, err)
	}
	if _, err := w.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if ok, err := readReady(int(r.Fd()), time.Second); err != nil || !ok {
		t.Fatalf("data pending: ready=%v err=%v, want true nil", ok, err)
	}
}
