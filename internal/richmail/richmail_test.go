package richmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestParseSource(t *testing.T) {
	tests := []struct {
		in   string
		kind SourceKind
	}{
		{"cid:hero", SourceCID},
		{"CID:hero", SourceCID},
		{"data:image/png;base64,iVBORw0KGgo=", SourceData},
		{"https://example.com/a.png", SourceRemote},
		{"http://example.com/a.png", SourceRemote},
		{"//example.com/a.png", SourceRemote},
		{"file:///etc/passwd", SourceUnknown},
		{"javascript:alert(1)", SourceUnknown},
		{"/relative/path.png", SourceUnknown},
		{"", SourceUnknown},
	}
	for _, tt := range tests {
		if got := ParseSource(tt.in).Kind; got != tt.kind {
			t.Errorf("ParseSource(%q).Kind = %v, want %v", tt.in, got, tt.kind)
		}
	}

	if got := ParseSource("<hero-image-123>"); got.Kind == SourceCID {
		t.Errorf("bare bracketed id should not be a CID: %+v", got)
	}
	if got := ParseSource("cid:  <Hero@Example> ").CID; got != "Hero@Example" {
		t.Errorf("CID = %q", got)
	}
}

func TestParseDataURI(t *testing.T) {
	pngData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	got := ParseSource("data:image/png;base64," + pngData)
	if got.Kind != SourceData {
		t.Fatalf("expected data source, got %v", got.Kind)
	}
	if len(got.Data) == 0 {
		t.Fatal("expected decoded data")
	}
	if _, err := Decode(got.Data, DefaultLimits()); err != nil {
		t.Fatalf("decoded data URI should be a valid png: %v", err)
	}

	for _, bad := range []string{
		"data:image/svg+xml;base64,PHN2Zz4=", // script-capable type refused
		"data:text/html,<b>x</b>",
		"data:image/png;base64,!!!not-base64!!!",
		"data:image/png;base64,",
	} {
		if s := ParseSource(bad); s.Kind != SourceUnknown {
			t.Errorf("ParseSource(%q).Kind = %v, want unknown", bad, s.Kind)
		}
	}
}

func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeLimits(t *testing.T) {
	data := tinyPNG(t, 16, 8)
	dec, err := Decode(data, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if dec.Width != 16 || dec.Height != 8 || dec.Format != "png" {
		t.Fatalf("decoded = %+v", dec)
	}

	if _, err := Decode(data, Limits{MaxBytes: 8}); err != ErrImageTooLarge {
		t.Errorf("expected ErrImageTooLarge, got %v", err)
	}
	if _, err := Decode(data, Limits{MaxDimension: 4}); err != ErrImageTooWide {
		t.Errorf("expected ErrImageTooWide, got %v", err)
	}
	if _, err := Decode(data, Limits{MaxPixels: 10}); err != ErrImageTooWide {
		t.Errorf("expected ErrImageTooWide for pixel cap, got %v", err)
	}
	if _, err := Decode([]byte("not an image"), DefaultLimits()); err == nil {
		t.Error("expected error for non-image")
	}
}

func TestLayoutPreservesAspectAndFits(t *testing.T) {
	geom := CellGeometry{CellWidthPx: 8, CellHeightPx: 16}
	in := Intrinsic{PixelWidth: 1200, PixelHeight: 600}
	p, ok := Layout(in, 70, geom)
	if !ok {
		t.Fatal("expected layout")
	}
	if p.Cols > 70 || p.Cols < 1 {
		t.Fatalf("cols = %d", p.Cols)
	}
	// aspect 2:1 means pixel height is half the pixel width, within the
	// unavoidable rounding to whole terminal cells.
	if ratio := float64(p.PixelHeight) / float64(p.PixelWidth); ratio < 0.45 || ratio > 0.55 {
		t.Fatalf("aspect not preserved: %dx%d", p.PixelWidth, p.PixelHeight)
	}
	if p.PixelWidth > 70*8 {
		t.Fatalf("image exceeds pane: %d", p.PixelWidth)
	}
}

func TestLayoutNeverEnlarges(t *testing.T) {
	geom := CellGeometry{CellWidthPx: 8, CellHeightPx: 16}
	p, ok := Layout(Intrinsic{PixelWidth: 16, PixelHeight: 16}, 100, geom)
	if !ok {
		t.Fatal("expected layout")
	}
	if p.PixelWidth > 16 || p.PixelHeight > 16 {
		t.Fatalf("tiny image was enlarged: %+v", p)
	}
}

func TestLayoutHonoursHintsAndMaxWidth(t *testing.T) {
	geom := CellGeometry{CellWidthPx: 8, CellHeightPx: 16}
	p, _ := Layout(Intrinsic{PixelWidth: 2400, PixelHeight: 1200, HintWidth: 600}, 200, geom)
	if p.PixelWidth > 600 {
		t.Fatalf("width hint ignored: %+v", p)
	}
	p, _ = Layout(Intrinsic{PixelWidth: 2400, PixelHeight: 1200, MaxWidth: 320}, 200, geom)
	if p.PixelWidth > 320 {
		t.Fatalf("max-width ignored: %+v", p)
	}
}

func TestLayoutCentering(t *testing.T) {
	geom := CellGeometry{CellWidthPx: 8, CellHeightPx: 16}
	p, _ := Layout(Intrinsic{PixelWidth: 80, PixelHeight: 80, Align: AlignCenter}, 40, geom)
	if p.OffsetCols+p.Cols > 40 || p.OffsetCols < 0 {
		t.Fatalf("centered image out of bounds: %+v", p)
	}
}

func TestLayoutRejectsDegenerate(t *testing.T) {
	if _, ok := Layout(Intrinsic{PixelWidth: 0, PixelHeight: 0}, 40, CellGeometry{}); ok {
		t.Error("zero-size image must not lay out")
	}
	if _, ok := Layout(Intrinsic{PixelWidth: 10, PixelHeight: 10}, 0, CellGeometry{}); ok {
		t.Error("zero-width pane must not lay out")
	}
}

func TestRemoteURLPolicy(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/x.png",
		"http://127.0.0.1:8080/x.png",
		"http://[::1]/x.png",
		"http://localhost/x.png",
		"http://10.0.0.1/x.png",
		"http://192.168.1.1/x.png",
		"http://172.16.5.4/x.png",
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/x.png",
		"http://[fc00::1]/x.png",
		"http://[fe80::1]/x.png",
		"http://100.64.0.1/x.png",
		"file:///etc/passwd",
		"ftp://example.com/x.png",
	}
	for _, raw := range blocked {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := ValidateRemoteURL(u); err == nil {
			t.Errorf("expected %q to be blocked", raw)
		}
	}

	allowed := []string{"https://example.com/a.png", "http://93.184.216.34/a.png"}
	for _, raw := range allowed {
		u, _ := url.Parse(raw)
		if err := ValidateRemoteURL(u); err != nil {
			t.Errorf("expected %q allowed, got %v", raw, err)
		}
	}
}

func TestCheckPublicIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "10.1.2.3", "192.168.0.5", "fe80::1", "fc00::1", "0.0.0.0"} {
		if err := CheckPublicIP(net.ParseIP(raw)); err == nil {
			t.Errorf("expected %s blocked", raw)
		}
	}
	// Note: low public addresses (e.g. 0.0.0.1) are not routable but are not
	// private; we only guarantee the ranges that matter for SSRF.
	if err := CheckPublicIP(net.ParseIP("1.1.1.1")); err != nil {
		t.Errorf("expected 1.1.1.1 allowed: %v", err)
	}
}

func TestRemoteFetchRejectsLocalBeforeConnecting(t *testing.T) {
	opts := DefaultRemoteOptions()
	opts.Timeout = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := opts.Fetch(ctx, "http://127.0.0.1:9/x.png"); err == nil {
		t.Fatal("expected loopback fetch to fail")
	}
}

func TestExtractImages(t *testing.T) {
	html := `<html><body>` +
		`<a href="https://shop.example/sale"><img src="cid:hero" width="1200" height="600" alt="Fall Sale"></a>` +
		`<table><tr><td><img src="cid:item1" alt="Boots"></td><td><img src="cid:item2" alt="Jacket"></td></tr></table>` +
		`<img src="https://cdn.example/track.gif" width="1" height="1" alt>` +
		`</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	m := ExtractImages(doc)
	if len(m.Images) != 4 {
		t.Fatalf("expected 4 images, got %d", len(m.Images))
	}
	hero, ok := m.Lookup(0)
	if !ok {
		t.Fatal("missing index 0")
	}
	if hero.Source.Kind != SourceCID || hero.Source.CID != "hero" {
		t.Errorf("hero source = %+v", hero.Source)
	}
	if hero.Alt != "Fall Sale" || hero.HintWidth != 1200 || hero.HintHeight != 600 {
		t.Errorf("hero = %+v", hero)
	}
	if hero.Link != "https://shop.example/sale" {
		t.Errorf("hero link = %q", hero.Link)
	}
	track, _ := m.Lookup(3)
	if !track.Tracking {
		t.Errorf("expected tracking pixel flagged: %+v", track)
	}
	if !m.HasMeaningfulImageContent() {
		t.Error("expected meaningful image content")
	}

	// The index attribute the markdown rule reads must be stamped.
	if got, _ := doc.Find("img").First().Attr("data-tidemail-image"); got != "0" {
		t.Errorf("data-tidemail-image = %q, want 0", got)
	}
}

func TestPartStoreDuplicateCIDKeepsFirst(t *testing.T) {
	store := NewPartStore([]Part{
		{ContentID: "dup", ContentType: "image/png", Data: []byte("FIRST")},
		{ContentID: "DUP", ContentType: "image/png", Data: []byte("SECOND")},
	})
	if data, ok := store.lookupCID("dup"); !ok || string(data) != "FIRST" {
		t.Fatalf("duplicate CID lookup = %q, ok=%v", data, ok)
	}
}

func TestResolveEmbedded(t *testing.T) {
	pngData := tinyPNG(t, 8, 8)
	store := NewPartStore([]Part{
		{ContentID: "hero", ContentType: "image/png", Data: pngData},
		{ContentLocation: "photo.png", ContentType: "image/png", Data: pngData},
	})
	limits := DefaultLimits()

	if got := ResolveEmbedded(Image{Source: ImageSource{Kind: SourceCID, CID: "hero"}}, store, limits); got.Status != ResolveOK || got.Decoded == nil {
		t.Errorf("cid resolve = %+v", got)
	}
	if got := ResolveEmbedded(Image{Source: ImageSource{Kind: SourceCID, CID: "missing"}}, store, limits); got.Status != ResolveEmbeddedMissing {
		t.Errorf("missing cid = %+v", got)
	}
	if got := ResolveEmbedded(Image{Source: ImageSource{Kind: SourceUnknown, Raw: "photo.png"}}, store, limits); got.Status != ResolveOK {
		t.Errorf("content-location resolve = %+v", got)
	}
	remote := Image{Source: ImageSource{Kind: SourceRemote, URL: "https://example.com/a.png"}}
	if got := ResolveEmbedded(remote, store, limits); got.Status != ResolveRemoteBlocked {
		t.Errorf("remote = %+v", got)
	}

	dataB64 := base64.StdEncoding.EncodeToString(pngData)
	if got := ResolveEmbedded(Image{Source: ParseSource("data:image/png;base64," + dataB64)}, store, limits); got.Status != ResolveOK {
		t.Errorf("data uri = %+v", got)
	}
	if got := ResolveEmbedded(Image{Source: ImageSource{Kind: SourceData, Data: []byte("garbage")}}, store, limits); got.Status != ResolveMalformed {
		t.Errorf("malformed data = %+v", got)
	}
}

func TestStripControl(t *testing.T) {
	got := stripControl("a\x1b]52;c;x\x07b\nc")
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("control bytes survived: %q", got)
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatalf("visible text lost: %q", got)
	}
}
