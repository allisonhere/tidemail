package ui

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/x/ansi"
)

const receiptTableFixture = `<table role="presentation"><tr><td>
<p>Order 1042</p><table>
<tr><td>Item</td><td>Qty</td><td>Amount</td></tr>
<tr><td>Coffee beans</td><td>2</td><td>$24.00</td></tr>
<tr><td>Paper filters</td><td>1</td><td>$6.50</td></tr>
<tr><td colspan="2">Subtotal</td><td>$30.50</td></tr>
<tr><td colspan="2">Tax</td><td>$2.44</td></tr>
<tr><td colspan="2">Total</td><td>$32.94</td></tr>
</table><p>Thank you for your order.</p></td></tr></table>`

func TestReceiptTableRecognition(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		want       bool
	}{
		{"headerless", `<table><tr><td>Coffee</td><td>$12.00</td></tr><tr><td>Total</td><td>$12.00</td></tr></table>`, true},
		{"presentationReceipt", `<table role="presentation"><tr><td>Coffee</td><td>€12,50</td></tr><tr><td>Total</td><td>€12,50</td></tr></table>`, true},
		{"quantity", `<table><tr><td>Paper</td><td>2</td><td>USD 5.00</td></tr><tr><td>Ink</td><td>1</td><td>USD 30.00</td></tr></table>`, true},
		{"tdHeaders", `<table><tr><td>Description</td><td>Amount</td></tr><tr><td>Plan</td><td>£20.00</td></tr></table>`, true},
		{"singleLayoutRow", `<table><tr><td>Replies</td><td>12</td></tr></table>`, false},
		{"newsletter", `<table><tr><td>Story one</td><td>Read more</td></tr><tr><td>Story two</td><td>Read more</td></tr></table>`, false},
		{"nested", `<table><tr><td><table><tr><td>Coffee</td><td>$12</td></tr><tr><td>Total</td><td>$12</td></tr></table></td></tr></table>`, false},
		{"images", `<table><tr><td><img alt="Coffee">Coffee</td><td>$12</td></tr><tr><td>Total</td><td>$12</td></tr></table>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatal(err)
			}
			if got := isReceiptTable(doc.Find("table").First()); got != tc.want {
				t.Fatalf("receipt=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestReceiptRenderingWideAndNarrow(t *testing.T) {
	for _, plain := range []bool{true, false} {
		for _, width := range []int{80, 40, 24} {
			got := ansi.Strip(renderHTMLBody(receiptTableFixture, width, CatppuccinMocha, plain))
			text := strings.Join(strings.Fields(got), " ")
			for _, want := range []string{"Coffee beans", "Paper filters", "Subtotal", "Tax", "Total", "$24.00", "$6.50", "$30.50", "$2.44", "$32.94"} {
				if strings.Count(text, want) != 1 {
					t.Fatalf("width=%d plain=%t: expected %q once:\n%s", width, plain, want, got)
				}
			}
			for _, line := range strings.Split(got, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width=%d overflow: %q", width, line)
				}
			}
			if width == 24 {
				for _, want := range []string{"Item: Coffee beans", "Qty: 2", "Amount: $24.00", "Total: $32.94"} {
					if !strings.Contains(got, want) {
						t.Fatalf("missing labelled record %q:\n%s", want, got)
					}
				}
			} else {
				if !strings.Contains(got, " | ") {
					t.Fatalf("expected columns:\n%s", got)
				}
				var amountEnd int
				for _, line := range strings.Split(got, "\n") {
					if strings.Contains(line, "$") {
						end := ansi.StringWidth(strings.TrimRight(line, " "))
						if amountEnd != 0 && amountEnd != end {
							t.Fatalf("amounts not aligned:\n%s", got)
						}
						amountEnd = end
					}
				}
			}
			t.Logf("width=%d plain=%t\n%s", width, plain, got)
		}
	}
}

func TestTableFallbackAndBounds(t *testing.T) {
	for _, span := range []string{`colspan="999999999"`, `colspan="-1"`, `rowspan="2"`} {
		html := `<table><tr><th>Name</th><th>Value</th></tr><tr><td ` + span + `>Keep me</td><td>$7.50</td></tr></table>`
		got := ansi.Strip(renderHTMLBody(html, 40, CatppuccinMocha, true))
		if !strings.Contains(got, "Keep me") || !strings.Contains(got, "$7.50") {
			t.Fatalf("fallback lost cells: %s", got)
		}
	}
	rows := [][]string{{"Item", "Qty", "Amount"}, {"日本茶 café 👩‍💻", "2", "€123,45"}, {strings.Repeat("verylong", 20), "1", "$1,234.56"}}
	for width := 2; width <= 80; width++ {
		for _, line := range renderTextTable(rows, width, true) {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width=%d overflow: %q", width, line)
			}
		}
	}
}

func TestHeaderlessReceiptNarrowPairs(t *testing.T) {
	got := strings.Join(renderTextTable([][]string{{"Coffee beans", "$12.00"}, {"Total", "$12.00"}}, 18, false), "\n")
	if !strings.Contains(got, "Total: $12.00") || strings.Contains(got, "Column") {
		t.Fatalf("expected label/value pairs: %s", got)
	}
}

func TestHeaderlessReceiptRendersThroughHTMLPipeline(t *testing.T) {
	html := `<table><tr><td>Coffee beans</td><td>€12,50</td></tr><tr><td>Discount</td><td>-€2,00</td></tr><tr><td>Total</td><td>€10,50</td></tr></table>`
	for _, width := range []int{80, 24} {
		got := ansi.Strip(renderHTMLBody(html, width, CatppuccinMocha, true))
		if width == 80 && !strings.Contains(got, " | ") {
			t.Fatalf("headerless receipt was flattened: %s", got)
		}
		if width == 24 && !strings.Contains(got, "Total: €10,50") {
			t.Fatalf("narrow receipt lost label/value relationship: %s", got)
		}
		for _, value := range []string{"€12,50", "-€2,00", "€10,50"} {
			if !strings.Contains(got, value) {
				t.Fatalf("amount changed: %s", got)
			}
		}
	}
}
