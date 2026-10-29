package mailapp

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// SignatureMarker is reserved in the interactive composer's body. CLI callers
// continue to use Prepare's explicit signature option instead.
const SignatureMarker = "⁂"

// PrepareCompose resolves the editable marker once, before queueing or PGP
// protection. A body without a marker is intentionally unsigned, including old
// drafts. The draft itself is never modified, so undo/edit restores the marker.
func PrepareCompose(account config.AccountConfig, draft db.Draft) (smtp.OutgoingMessage, error) {
	body, html, err := composeBody(draft.BodyText, account.Signature)
	if err != nil {
		return smtp.OutgoingMessage{}, err
	}
	// Reuse all ordinary identity, recipient, and attachment validation without
	// rendering the original Markdown twice or appending another signature.
	draft.BodyText = ""
	msg, err := Prepare(account, draft, false)
	if err != nil {
		return msg, err
	}
	msg.Body, msg.HTMLBody = body, html
	return msg, nil
}

func composeBody(body, signature string) (string, string, error) {
	var html string
	switch strings.Count(body, SignatureMarker) {
	case 0:
		html = smtp.MarkdownToHTML(body)
	case 1:
		body = strings.ReplaceAll(body, "\r\n", "\n")
		pos := strings.Index(body, SignatureMarker)
		start := strings.LastIndex(body[:pos], "\n") + 1
		end := len(body)
		if next := strings.IndexByte(body[pos:], '\n'); next >= 0 {
			end = pos + next
		}
		if strings.Trim(body[start:end], " \t") != SignatureMarker {
			return "", "", fmt.Errorf("place ⁂ alone on a line, or delete it to omit the signature")
		}
		source := []byte(body)
		md := goldmark.New(goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(signatureRenderer{}, 100))))
		doc := md.Parser().Parse(text.NewReader(source))
		var marker ast.Node
		for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
			if node.Kind() != ast.KindParagraph || node.Lines().Len() != 1 {
				continue
			}
			line := node.Lines().At(0)
			if line.Start <= pos && pos < line.Stop && strings.TrimSpace(string(line.Value(source))) == SignatureMarker {
				marker = node
				break
			}
		}
		if marker == nil {
			return "", "", fmt.Errorf("put ⁂ in its own paragraph outside quotes, lists, and code blocks (add blank lines), or delete it")
		}
		sig := strings.TrimSpace(signature)
		doc.ReplaceChild(doc, marker, &signatureBlock{signature: sig})
		var rendered bytes.Buffer
		if err := md.Renderer().Render(&rendered, source, doc); err != nil {
			return "", "", err
		}
		html = rendered.String()
		if end < len(body) {
			end++
		} // consume the marker line's newline, not its neighbors
		replacement := ""
		if sig != "" {
			replacement = "-- \n" + sig + "\n"
		}
		body = body[:start] + replacement + body[end:]
	default:
		return "", "", fmt.Errorf("keep only one ⁂ signature marker, or delete all markers to send unsigned")
	}
	// This also catches a reserved rune introduced by an HTML entity or the
	// configured signature, rather than only checking literal Markdown input.
	if strings.Contains(body, SignatureMarker) || strings.Contains(html, SignatureMarker) {
		return "", "", fmt.Errorf("remove the reserved ⁂ symbol from message text or the configured signature")
	}
	return body, html, nil
}

var signatureBlockKind = ast.NewNodeKind("ComposeSignature")

type signatureBlock struct {
	ast.BaseBlock
	signature string
}

func (n *signatureBlock) Kind() ast.NodeKind            { return signatureBlockKind }
func (n *signatureBlock) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type signatureRenderer struct{}

func (signatureRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(signatureBlockKind, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, err := w.WriteString(smtp.SignatureHTML(node.(*signatureBlock).signature))
			return ast.WalkSkipChildren, err
		}
		return ast.WalkContinue, nil
	})
}
