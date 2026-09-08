package imagepreview

import (
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const imageID = 8675309

// Terminal implements tea.ExecCommand. Bubble Tea releases input and rendering
// before Run and restores both afterward, so graphics never race its renderer.
type Terminal struct{ Image Image }

func (*Terminal) SetStdin(io.Reader)  {}
func (*Terminal) SetStdout(io.Writer) {}
func (*Terminal) SetStderr(io.Writer) {}

func (p *Terminal) Run() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("image previews need an interactive terminal")
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, state)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	return runPreview(&ttyScreen{tty}, p.Image, signals)
}

type screenSize struct{ cols, rows, pixelWidth, pixelHeight int }
type previewScreen interface {
	io.Writer
	read(time.Duration) (string, error)
	size() (screenSize, error)
}
type ttyScreen struct{ *os.File }

func (t *ttyScreen) read(timeout time.Duration) (string, error) {
	fds := []unix.PollFd{{Fd: int32(t.Fd()), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, int(timeout.Milliseconds()))
	if err == unix.EINTR {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
		return "", io.EOF
	}
	var buf [1024]byte
	count, err := t.Read(buf[:])
	return string(buf[:count]), err
}
func (t *ttyScreen) size() (screenSize, error) {
	ws, err := unix.IoctlGetWinsize(int(t.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return screenSize{}, err
	}
	return screenSize{int(ws.Col), int(ws.Row), int(ws.Xpixel), int(ws.Ypixel)}, nil
}

func runPreview(screen previewScreen, img Image, signals <-chan os.Signal) error {
	// Own a temporary screen; delete only our own image, even on failed probes.
	defer fmt.Fprintf(screen, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\\x1b[?25h\x1b[?1049l", imageID)
	if _, err := io.WriteString(screen, "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(screen, "\x1b_Gi=%d,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\", imageID); err != nil {
		return err
	}
	response := ""
	supported := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		select {
		case <-signals:
			return nil
		default:
		}
		input, err := screen.read(100 * time.Millisecond)
		if err != nil {
			return err
		}
		response += input
		if strings.Contains(response, fmt.Sprintf("\x1b_Gi=%d;OK\x1b\\", imageID)) {
			supported = true
			break
		}
		if len(response) > 4096 || strings.Contains(response, "\x03") {
			break
		}
	}
	if !supported {
		return fmt.Errorf("this terminal does not support image previews; image descriptions and Save attachments are still available")
	}
	if err := transmitPNG(screen, img.PNG); err != nil {
		return err
	}
	previous := screenSize{}
	for {
		select {
		case <-signals:
			return nil
		default:
		}
		size, err := screen.size()
		if err != nil {
			return err
		}
		if size != previous {
			if err := placeImage(screen, img, size); err != nil {
				return err
			}
			previous = size
		}
		input, err := screen.read(100 * time.Millisecond)
		if err != nil {
			return err
		}
		// Graphics commands suppress replies after the probe. Escape closes the
		// preview, as do q and Ctrl+C; no input goroutine survives this session.
		if strings.ContainsAny(input, "\x1bq\x03") {
			return nil
		}
	}
}

func transmitPNG(out io.Writer, png []byte) error {
	encoded := base64.StdEncoding.EncodeToString(png)
	for offset := 0; offset < len(encoded); offset += 4096 {
		end := min(offset+4096, len(encoded))
		more := 1
		if end == len(encoded) {
			more = 0
		}
		metadata := ""
		if offset == 0 {
			metadata = fmt.Sprintf("a=t,f=100,t=d,i=%d,", imageID)
		}
		if _, err := fmt.Fprintf(out, "\x1b_G%sq=2,m=%d;%s\x1b\\", metadata, more, encoded[offset:end]); err != nil {
			return err
		}
	}
	return nil
}

func fitImage(img Image, size screenSize) (cols, rows int) {
	availableCols, availableRows := max(1, size.cols-2), max(1, size.rows-3)
	cellWidth, cellHeight := 8.0, 16.0
	if size.pixelWidth > 0 && size.pixelHeight > 0 && size.cols > 0 && size.rows > 0 {
		cellWidth = float64(size.pixelWidth) / float64(size.cols)
		cellHeight = float64(size.pixelHeight) / float64(size.rows)
	}
	scale := math.Min(float64(availableCols)*cellWidth/float64(img.Width), float64(availableRows)*cellHeight/float64(img.Height))
	return max(1, int(math.Ceil(float64(img.Width)*scale/cellWidth))), max(1, int(math.Ceil(float64(img.Height)*scale/cellHeight)))
}
func placeImage(out io.Writer, img Image, size screenSize) error {
	cols, rows := fitImage(img, size)
	cols, rows = min(cols, max(1, size.cols-2)), min(rows, max(1, size.rows-3))
	// Remove placements only, retaining transmitted pixels for resize.
	if _, err := fmt.Fprintf(out, "\x1b_Ga=d,d=i,i=%d,q=2;\x1b\\\x1b[H\x1b[2K", imageID); err != nil {
		return err
	}
	if size.cols < 4 || size.rows < 4 {
		return nil
	}
	hint := "Image preview — Esc to return"
	if size.cols < 32 {
		hint = "Esc: return"
	}
	if size.cols < 12 {
		hint = "Esc"
	}
	if _, err := io.WriteString(out, hint); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "\x1b[3;%dH\x1b_Ga=p,i=%d,p=1,c=%d,r=%d,C=1,q=2;\x1b\\", max(1, (size.cols-cols)/2+1), imageID, cols, rows)
	return err
}
