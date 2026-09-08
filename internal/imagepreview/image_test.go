package imagepreview

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func samplePNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestDecodeFormatsAndLimits(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			var b bytes.Buffer
			switch format {
			case "png":
				b.Write(samplePNG(t))
			case "jpeg":
				if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 12, 8)), nil); err != nil {
					t.Fatal(err)
				}
			case "gif":
				frame := image.NewPaletted(image.Rect(0, 0, 12, 8), color.Palette{color.Black, color.White})
				if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{1, 1}}); err != nil {
					t.Fatal(err)
				}
			}
			decoded, err := Decode(b.Bytes())
			if err != nil || decoded.Width != 12 || decoded.Height != 8 {
				t.Fatalf("decode: %+v %v", decoded, err)
			}
		})
	}
	for _, data := range [][]byte{[]byte("<svg/>"), make([]byte, MaxBytes+1), samplePNG(t)[:30]} {
		if _, err := Decode(data); err == nil {
			t.Fatal("accepted invalid or oversized image")
		}
	}
	huge := samplePNG(t)
	binary.BigEndian.PutUint32(huge[16:20], 100_000)
	binary.BigEndian.PutUint32(huge[20:24], 100_000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	if _, err := Decode(huge); err == nil || !strings.Contains(err.Error(), "pixels") {
		t.Fatalf("dimension limit: %v", err)
	}
}
func TestFetchValidationAndCancellation(t *testing.T) {
	png := samplePNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" {
			t.Error("request leaked credentials")
		}
		switch r.URL.Path {
		case "/ok":
			w.Write(png)
		case "/large":
			w.Header().Set("Content-Length", "10485761")
		case "/stream-large":
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			w.Write(make([]byte, MaxBytes+1))
		case "/redirect":
			http.Redirect(w, r, "/ok", 302)
		case "/loop":
			http.Redirect(w, r, "/loop", 302)
		case "/badredirect":
			http.Redirect(w, r, "file:///tmp/image.png", 302)
		case "/invalid":
			w.Write([]byte("broken"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/ok", "/redirect"} {
		if _, err := Fetch(context.Background(), server.URL+path); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{server.URL + "/large", server.URL + "/stream-large", server.URL + "/loop", server.URL + "/badredirect", server.URL + "/invalid", server.URL + "/missing", "file:///tmp/a", "https://user:pass@example.com/a"} {
		if _, err := Fetch(context.Background(), source); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fetch(ctx, server.URL+"/ok"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
