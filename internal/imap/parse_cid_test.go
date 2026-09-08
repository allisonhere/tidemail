package imap

import "testing"

func TestParseBodyPreservesEmbeddedContentID(t *testing.T) {
	raw := []byte("MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=images\r\n\r\n" +
		"--images\r\nContent-Type: text/html\r\n\r\n<p>Chart</p><img src=\"cid:chart@mail\">\r\n" +
		"--images\r\nContent-Type: image/png; name=chart.png\r\nContent-ID: <chart@mail>\r\nContent-Disposition: inline\r\nContent-Transfer-Encoding: base64\r\n\r\naW1hZ2U=\r\n--images--\r\n")
	_, _, atts := parseBody(raw)
	if len(atts) != 1 || atts[0].ContentID != "chart@mail" || string(atts[0].Data) != "image" {
		t.Fatalf("CID lost: %+v", atts)
	}
}
