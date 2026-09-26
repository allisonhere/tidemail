package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/x/ansi"
)

// Amounts and quantities are evidence of tabular data, not a reason to turn
// arbitrary multi-column newsletter layouts into tables. Accept common decimal
// and currency formats without interpreting or changing their values.
var tableNumberRE = regexp.MustCompile(`^(?:[A-Z]{3}\s*)?[-+−(]?\s*[$€£¥₹₩]?\s*[0-9]+(?:[., '\x{00a0}][0-9]+)*\s*(?:%|[$€£¥₹₩]|[A-Z]{3})?\)?$`)

func tableNumber(s string) bool {
	return tableNumberRE.MatchString(strings.TrimSpace(s))
}

// emailTableRows preserves column positions for colspan totals. Rowspans and
// very large/malformed grids keep the existing flattened fallback; never
// allocate a grid based on an unchecked attribute from email HTML.
func emailTableRows(table *goquery.Selection) ([][]string, bool) {
	var rows [][]string
	valid := true
	table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if !valid || !selectionBelongsToTable(tr, table) {
			return
		}
		var row []string
		tr.ChildrenFiltered("th,td").Each(func(_ int, cell *goquery.Selection) {
			if !valid {
				return
			}
			if raw, exists := cell.Attr("rowspan"); exists && strings.TrimSpace(raw) != "1" {
				valid = false
				return
			}
			span := 1
			if raw, exists := cell.Attr("colspan"); exists {
				n, err := strconv.Atoi(strings.TrimSpace(raw))
				if err != nil || n < 1 || n > 32 {
					valid = false
					return
				}
				span = n
			}
			if len(row)+span > 32 {
				valid = false
				return
			}
			row = append(row, tableCellText(cell))
			for i := 1; i < span; i++ {
				row = append(row, "")
			}
		})
		if len(row) > 0 {
			rows = append(rows, row)
		}
	})
	return rows, valid
}

func receiptHeader(row []string) bool {
	if len(row) < 2 {
		return false
	}
	for _, cell := range row {
		switch strings.ToLower(strings.TrimSpace(cell)) {
		case "item", "product", "description", "qty", "quantity", "price", "unit price", "amount", "total", "cost":
		default:
			return false
		}
	}
	return true
}

func emailTableHasHeader(table *goquery.Selection, rows [][]string) bool {
	if len(rows) == 0 {
		return false
	}
	first := table.Find("tr").FilterFunction(func(_ int, tr *goquery.Selection) bool {
		return selectionBelongsToTable(tr, table) && tr.ChildrenFiltered("th,td").Length() > 0
	}).First()
	cells := first.ChildrenFiltered("th,td")
	return cells.Length() > 0 && cells.Filter("th").Length() == cells.Length() || receiptHeader(rows[0])
}

func isReceiptTable(table *goquery.Selection) bool {
	// Nested tables, illustrations, headings and buttons indicate presentation
	// layout. Links inside an item description are fine.
	if table.Find("table,[data-tidemail-layout-table],img,h1,h2,h3,h4,h5,h6,button,[role=button]").Length() > 0 {
		return false
	}
	rows, ok := emailTableRows(table)
	if !ok || len(rows) < 2 {
		return false
	}
	start := 0
	if receiptHeader(rows[0]) {
		start = 1
	}
	matched := 0
	for _, row := range rows[start:] {
		if len(row) < 2 || len(row) > 4 {
			return false
		}
		label := strings.TrimSpace(row[0])
		if label == "" || tableNumber(label) || len(label) > 240 || !tableNumber(row[len(row)-1]) {
			return false
		}
		for _, cell := range row[1 : len(row)-1] {
			if strings.TrimSpace(cell) != "" && !tableNumber(cell) {
				return false
			}
		}
		matched++
	}
	return matched >= 2 || start == 1 && matched == 1
}

func tableNumericColumns(rows [][]string, cols int, header bool) []bool {
	numeric := make([]bool, cols)
	start := 0
	if header {
		start = 1
	}
	for col := 0; col < cols; col++ {
		seen, valid := false, true
		for _, row := range rows[start:] {
			if col >= len(row) || strings.TrimSpace(row[col]) == "" {
				continue
			}
			seen = true
			valid = valid && tableNumber(row[col])
		}
		numeric[col] = seen && valid
	}
	return numeric
}

// renderStackedTable repeats actual column headings for each record. Headerless
// two-cell rows become label/value pairs; wider unlabelled data uses neutral
// column numbers so no meanings or prices are invented.
func renderStackedTable(rows [][]string, width int, header bool) []string {
	var lines []string
	var headings []string
	if header && len(rows) > 1 {
		headings, rows = rows[0], rows[1:]
	}
	appendField := func(text string) {
		lines = append(lines, strings.Split(ansi.Hardwrap(ansi.Wordwrap(text, width, ""), width, true), "\n")...)
	}
	for _, row := range rows {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		// A merged summary cell followed by an amount remains a single pair.
		var nonempty []string
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				nonempty = append(nonempty, cell)
			}
		}
		if len(nonempty) == 2 && (len(headings) == 0 && len(row) == 2 || len(row) > 2 && len(nonempty) < len(row)) {
			appendField(nonempty[0] + ": " + nonempty[1])
			continue
		}
		for col, cell := range row {
			if strings.TrimSpace(cell) == "" {
				continue
			}
			label := fmt.Sprintf("Column %d", col+1)
			if col < len(headings) && strings.TrimSpace(headings[col]) != "" {
				label = headings[col]
			}
			appendField(label + ": " + cell)
		}
	}
	return lines
}
