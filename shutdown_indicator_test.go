package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderShutdownFrameUsesShimmerColors(t *testing.T) {
	frame := renderShutdownFrame(3)
	if !strings.HasPrefix(frame, "\r\x1b[2K") {
		t.Fatalf("frame does not clear the current terminal line: %q", frame)
	}
	if !strings.Contains(frame, "\x1b[38;2;") {
		t.Fatalf("frame does not contain true-color shimmer styling: %q", frame)
	}
	if !strings.HasSuffix(frame, "\x1b[0m") {
		t.Fatalf("frame does not reset terminal styling: %q", frame)
	}
}

func TestShutdownIndicatorLeavesStableMessage(t *testing.T) {
	var output bytes.Buffer
	indicator := startShutdownIndicatorOn(&output, nil, time.Hour)
	indicator.Stop()

	if got := output.String(); !strings.HasSuffix(got, "\r\x1b[2K"+shutdownMessage+"\x1b[0m\n") {
		t.Fatalf("indicator did not finish with the quit message: %q", got)
	}
}

type trackedShutdownWriter struct {
	bytes.Buffer
	closed bool
}

func (w *trackedShutdownWriter) Close() error {
	w.closed = true
	return nil
}

func TestShutdownIndicatorClosesOwnedWriterAfterFinalMessage(t *testing.T) {
	output := &trackedShutdownWriter{}
	indicator := startShutdownIndicatorOn(output, output, time.Hour)
	indicator.Stop()

	if !output.closed {
		t.Fatal("expected the owned terminal writer to be closed")
	}
	if !strings.HasSuffix(output.String(), shutdownMessage+"\x1b[0m\n") {
		t.Fatal("expected the final message to be written before the terminal closes")
	}
}
