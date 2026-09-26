package termimage

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
)

// placeholderRune is the Kitty Unicode placeholder character. Every image cell
// is this rune followed by combining diacritics that encode its row, column,
// and the high byte of the image id.
const placeholderRune = '\U0010EEEE'

// kittyChunkSize is the protocol's maximum base64 payload per escape.
const kittyChunkSize = 4096

// rowColumnDiacritics is Kitty's fixed table mapping index -> combining mark.
// Index 0 is U+0305 and index 296 is the last valid value. Transcribed from
// kitty's gen/rowcolumn-diacritics.txt.
var rowColumnDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A, 0x034B, 0x034C,
	0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592,
	0x0593, 0x0594, 0x0595, 0x0597, 0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2, 0x06E4, 0x06E7, 0x06E8, 0x06EB,
	0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3,
	0x0816, 0x0817, 0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C, 0x082D, 0x0951, 0x0953, 0x0954,
	0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71,
	0x1B72, 0x1B73, 0x1CD0, 0x1CD1, 0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5,
	0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1, 0x20D4, 0x20D5, 0x20D6, 0x20D7,
	0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED, 0x2DEE,
	0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1,
	0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1,
	0xFE20, 0xFE21, 0xFE22, 0xFE23, 0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186,
	0x1D187, 0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243, 0x1D244,
}

// MaxGridValue is the largest row or column index the diacritic table can
// encode. A grid larger than this cannot use Unicode placeholders.
const MaxGridValue = 296

// KittyBackend renders via the Kitty graphics protocol's Unicode placeholders.
type KittyBackend struct {
	caps Capabilities
}

// NewKittyBackend builds a backend with the given capabilities.
func NewKittyBackend(caps Capabilities) *KittyBackend { return &KittyBackend{caps: caps} }

// Capabilities returns the backend's terminal capabilities.
func (b *KittyBackend) Capabilities() Capabilities { return b.caps }

// SupportsGrid reports whether a cols x rows placeholder grid is encodable.
func SupportsGrid(cols, rows int) bool {
	return cols >= 1 && rows >= 1 && cols <= MaxGridValue+1 && rows <= MaxGridValue+1
}

// Transmit uploads img with a virtual placement of cols x rows cells. The image
// is scaled down to pxWidth x pxHeight first; it is never enlarged. The returned
// bytes are self-contained escape sequences safe to write to the terminal.
func (b *KittyBackend) Transmit(id uint32, img image.Image, cols, rows, pxWidth, pxHeight int) ([]byte, bool) {
	if img == nil || !SupportsGrid(cols, rows) {
		return nil, false
	}
	if _, _, ok := idParts(id); !ok {
		return nil, false
	}
	scaled := scaleForUpload(img, pxWidth, pxHeight)
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, scaled); err != nil {
		return nil, false
	}
	control := fmt.Sprintf("a=T,f=100,i=%d,U=1,c=%d,r=%d,q=2", id, cols, rows)
	return kittyUpload(control, pngBuf.Bytes()), true
}

// Delete builds the escape that removes an image id (and its virtual placement)
// from the terminal, so switching messages does not leak terminal memory.
func (b *KittyBackend) Delete(id uint32) []byte {
	if _, _, ok := idParts(id); !ok {
		return nil
	}
	return []byte(fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", id))
}

// PlaceholderRows builds the text rows that display an already-transmitted
// image. Each row is exactly cols visible cells wide and carries its own
// foreground color/reset, so it can be spliced directly into the viewport.
func (b *KittyBackend) PlaceholderRows(id uint32, cols, rows int) []string {
	if !SupportsGrid(cols, rows) {
		return nil
	}
	low, hi, ok := idParts(id)
	if !ok {
		return nil
	}
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		sb.Grow(cols*4 + 16)
		fmt.Fprintf(&sb, "\x1b[38;5;%dm", low)
		for c := 0; c < cols; c++ {
			sb.WriteRune(placeholderRune)
			sb.WriteRune(rowColumnDiacritics[r])
			sb.WriteRune(rowColumnDiacritics[c])
			sb.WriteRune(rowColumnDiacritics[hi])
		}
		sb.WriteString("\x1b[39m")
		out[r] = sb.String()
	}
	return out
}

// kittyUpload chunks a base64 payload into protocol-legal escapes. The first
// escape carries the control data; continuation chunks carry only m.
func kittyUpload(control string, data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)
	var out bytes.Buffer
	first := true
	for len(encoded) > 0 {
		n := kittyChunkSize
		if n > len(encoded) {
			n = len(encoded)
		}
		chunk := encoded[:n]
		encoded = encoded[n:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&out, "\x1b_G%s,m=%d;%s\x1b\\", control, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return out.Bytes()
}

// idParts splits an id into the low byte (foreground color) and high byte
// (third diacritic). A zero low byte is invalid: it would collide with the
// terminal default foreground.
func idParts(id uint32) (low, hi int, ok bool) {
	low = int(id & 0xff)
	hi = int((id >> 24) & 0xff)
	if low == 0 || low > 255 {
		return 0, 0, false
	}
	if hi > MaxGridValue {
		return 0, 0, false
	}
	return low, hi, true
}

// MakeID composes an image id from its low and high bytes.
func MakeID(low, hi int) uint32 {
	return uint32(hi)<<24 | uint32(low&0xff)
}
