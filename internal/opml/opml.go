package opml

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type OPML struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    Head     `xml:"head"`
	Body    Body     `xml:"body"`
}

type Head struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated,omitempty"`
}

type Body struct {
	Outlines []Outline `xml:"outline"`
}

type Outline struct {
	Text    string `xml:"text,attr"`
	Title   string `xml:"title,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	XMLURL  string `xml:"xmlUrl,attr,omitempty"`
	HTMLURL string `xml:"htmlUrl,attr,omitempty"`
}

func Import(path string) ([]Outline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read opml: %w", err)
	}

	var o OPML
	if err := xml.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("parse opml: %w", err)
	}

	// Filter to only feed outlines (have xmlUrl)
	var feeds []Outline
	for _, outline := range o.Body.Outlines {
		if outline.XMLURL != "" {
			feeds = append(feeds, outline)
		}
		// Some OPML files nest feeds under category outlines
		// (not handled here — flat OPML only)
	}
	return feeds, nil
}

type Feed interface {
	GetID() int64
	GetURL() string
	GetTitle() string
}

func Export(feeds []Outline, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	o := OPML{
		Version: "2.0",
		Head: Head{
			Title:       "RSS Reader Feeds",
			DateCreated: time.Now().Format(time.RFC1123),
		},
		Body: Body{Outlines: feeds},
	}

	data, err := xml.MarshalIndent(o, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal opml: %w", err)
	}

	content := append([]byte(xml.Header), data...)
	return os.WriteFile(path, content, 0o644)
}

func ExportPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return filepath.Join(xdg, "rss", "feeds.opml"), nil
}
