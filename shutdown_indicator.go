package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const shutdownMessage = "Safely Quitting..."

type shutdownIndicator struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func startShutdownIndicator() *shutdownIndicator {
	w := io.Writer(os.Stdout)
	var closer io.Closer
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		w = tty
		closer = tty
	}
	return startShutdownIndicatorOn(w, closer, 85*time.Millisecond)
}

func startShutdownIndicatorOn(w io.Writer, closer io.Closer, interval time.Duration) *shutdownIndicator {
	indicator := &shutdownIndicator{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(indicator.done)
		if closer != nil {
			defer closer.Close()
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		frame := 0
		_, _ = io.WriteString(w, renderShutdownFrame(frame))
		for {
			select {
			case <-ticker.C:
				frame++
				_, _ = io.WriteString(w, renderShutdownFrame(frame))
			case <-indicator.stop:
				_, _ = fmt.Fprintf(w, "\r\x1b[2K%s\x1b[0m\n", shutdownMessage)
				return
			}
		}
	}()
	return indicator
}

func (i *shutdownIndicator) Stop() {
	i.once.Do(func() {
		close(i.stop)
		<-i.done
	})
}

func renderShutdownFrame(frame int) string {
	runes := []rune(shutdownMessage)
	position := frame % (len(runes) + 5)
	line := "\r\x1b[2K"
	for idx, r := range runes {
		distance := idx - position
		if distance < 0 {
			distance = -distance
		}
		brightness := max(0, 4-distance)
		red := 100 + brightness*35
		green := 185 + brightness*17
		blue := 205 + brightness*12
		line += fmt.Sprintf("\x1b[38;2;%d;%d;%dm%c", red, green, blue, r)
	}
	return line + "\x1b[0m"
}
