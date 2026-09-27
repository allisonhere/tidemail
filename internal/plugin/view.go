package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Structured views: a report's final output as semantic blocks (stat tiles,
// sparklines, bars, heatmaps, tables, text) that TideMail lays out and colors
// from the user's theme. Plugins say what the data is, never how it looks:
// there are no color, style, or escape fields, and every string is plain text.

// ViewVersion is the structured-view format this TideMail draws. It is sent
// to reports as context.views so a plugin can fall back to a text report on
// an older TideMail.
const ViewVersion = 1

// Block types.
const (
	BlockStats     = "stats"
	BlockSparkline = "sparkline"
	BlockBars      = "bars"
	BlockHeatmap   = "heatmap"
	BlockTable     = "table"
	BlockText      = "text"
)

// BlockTypes lists every block type in documentation order.
var BlockTypes = []string{BlockStats, BlockSparkline, BlockBars, BlockHeatmap, BlockTable, BlockText}

// Tones say what a number means; TideMail picks the color.
const (
	ToneNeutral   = "neutral"
	TonePositive  = "positive"
	ToneAttention = "attention"
	ToneCritical  = "critical"
)

// Tones lists every tone.
var Tones = []string{ToneNeutral, TonePositive, ToneAttention, ToneCritical}

// BarsCategory marks bars whose labels are mail categories; TideMail colors
// them like category tags.
const BarsCategory = "category"

// View limits.
const (
	MaxViewBlocks      = 32
	MaxViewTitleLen    = 80
	MaxViewLabelLen    = 40
	MaxViewValueLen    = 24
	MaxViewStats       = 8
	MaxViewSeries      = 3
	MaxViewPoints      = 400
	MaxViewBars        = 24
	MaxViewHeatRows    = 12
	MaxViewHeatColumns = 60
	MaxViewTableCols   = 6
	MaxViewTableRows   = 50
	MaxViewCellLen     = 120
	MaxViewTextLen     = 4000
)

// View is a validated structured report.
type View struct {
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	Blocks   []Block `json:"blocks"`
}

// Block is one section of a view. Only the fields of its Type are set.
type Block struct {
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	Note  string `json:"note,omitempty"`

	Items  []ViewItem   `json:"items,omitempty"`  // stats, bars
	Kind   string       `json:"kind,omitempty"`   // bars
	Series []ViewSeries `json:"series,omitempty"` // sparkline
	Start  string       `json:"start,omitempty"`  // sparkline axis labels
	End    string       `json:"end,omitempty"`

	Columns []string   `json:"columns,omitempty"` // heatmap, table
	Rows    []ViewRow  `json:"rows,omitempty"`    // heatmap
	Cells   [][]string `json:"cells,omitempty"`   // table
	Text    string     `json:"text,omitempty"`    // text
}

// ViewItem is one stat tile or bar.
type ViewItem struct {
	Label string  `json:"label"`
	Value string  `json:"value,omitempty"` // stats: the displayed value
	Count float64 `json:"count,omitempty"` // bars: the bar length
	Note  string  `json:"note,omitempty"`
	Tone  string  `json:"tone,omitempty"`
}

// ViewSeries is one sparkline.
type ViewSeries struct {
	Label  string    `json:"label"`
	Values []float64 `json:"values"`
}

// ViewRow is one heatmap row.
type ViewRow struct {
	Label  string    `json:"label"`
	Values []float64 `json:"values"`
}

// blockFields lists the fields each block type accepts besides "type".
var blockFields = map[string]map[string]bool{
	BlockStats:     set("title", "note", "items"),
	BlockSparkline: set("title", "note", "series", "start", "end"),
	BlockBars:      set("title", "note", "items", "kind"),
	BlockHeatmap:   set("title", "note", "columns", "rows"),
	BlockTable:     set("title", "note", "columns", "cells"),
	BlockText:      set("title", "text"),
}

// ParseView validates a report's view. Unknown fields, block types, and
// tones are errors, so a view never silently loses content.
func ParseView(raw json.RawMessage) (View, error) {
	var shape struct {
		Title    string            `json:"title"`
		Subtitle string            `json:"subtitle"`
		Blocks   []json.RawMessage `json:"blocks"`
	}
	if err := strictDecode(raw, &shape); err != nil {
		return View{}, fmt.Errorf("view: %w", err)
	}
	if err := viewText("view title", shape.Title, MaxViewTitleLen, true); err != nil {
		return View{}, err
	}
	if err := viewText("view subtitle", shape.Subtitle, MaxViewTitleLen, false); err != nil {
		return View{}, err
	}
	if len(shape.Blocks) == 0 || len(shape.Blocks) > MaxViewBlocks {
		return View{}, fmt.Errorf("view: blocks must list 1 to %d blocks", MaxViewBlocks)
	}
	v := View{Title: shape.Title, Subtitle: shape.Subtitle}
	for i, rawBlock := range shape.Blocks {
		b, err := parseBlock(rawBlock)
		if err != nil {
			return View{}, fmt.Errorf("view block %d: %w", i+1, err)
		}
		v.Blocks = append(v.Blocks, b)
	}
	return v, nil
}

func parseBlock(raw json.RawMessage) (Block, error) {
	var keys map[string]json.RawMessage
	if err := strictDecode(raw, &keys); err != nil || keys == nil {
		return Block{}, errors.New("a block must be a JSON object")
	}
	var typ string
	_ = json.Unmarshal(keys["type"], &typ)
	allowed, ok := blockFields[typ]
	if !ok {
		return Block{}, fmt.Errorf("unknown block type %q (supported: %s)", typ, strings.Join(BlockTypes, ", "))
	}
	for _, key := range sortedKeys(keysOf(keys)) {
		if key != "type" && !allowed[key] {
			return Block{}, fmt.Errorf("%s blocks do not accept %q", typ, key)
		}
	}
	var b Block
	if err := strictDecode(raw, &b); err != nil {
		return Block{}, err
	}
	if err := viewText("title", b.Title, MaxViewTitleLen, false); err != nil {
		return Block{}, err
	}
	if err := viewText("note", b.Note, MaxViewTitleLen, false); err != nil {
		return Block{}, err
	}
	switch typ {
	case BlockStats:
		return b, checkItems(b.Items, MaxViewStats, true)
	case BlockBars:
		if b.Kind != "" && b.Kind != BarsCategory {
			return Block{}, fmt.Errorf("bars kind %q is not %q", b.Kind, BarsCategory)
		}
		return b, checkItems(b.Items, MaxViewBars, false)
	case BlockSparkline:
		if len(b.Series) == 0 || len(b.Series) > MaxViewSeries {
			return Block{}, fmt.Errorf("series must list 1 to %d series", MaxViewSeries)
		}
		for _, s := range b.Series {
			if err := viewText("series label", s.Label, MaxViewLabelLen, false); err != nil {
				return Block{}, err
			}
			if err := checkValues(s.Values, MaxViewPoints); err != nil {
				return Block{}, err
			}
		}
		for _, l := range []string{b.Start, b.End} {
			if err := viewText("axis label", l, MaxViewLabelLen, false); err != nil {
				return Block{}, err
			}
		}
	case BlockHeatmap:
		if len(b.Rows) == 0 || len(b.Rows) > MaxViewHeatRows {
			return Block{}, fmt.Errorf("rows must list 1 to %d rows", MaxViewHeatRows)
		}
		if len(b.Columns) > MaxViewHeatColumns {
			return Block{}, fmt.Errorf("at most %d columns", MaxViewHeatColumns)
		}
		for _, c := range b.Columns {
			if err := viewText("column", c, MaxViewLabelLen, false); err != nil {
				return Block{}, err
			}
		}
		for _, r := range b.Rows {
			if err := viewText("row label", r.Label, MaxViewLabelLen, false); err != nil {
				return Block{}, err
			}
			if err := checkValues(r.Values, MaxViewHeatColumns); err != nil {
				return Block{}, err
			}
		}
	case BlockTable:
		if len(b.Columns) == 0 || len(b.Columns) > MaxViewTableCols {
			return Block{}, fmt.Errorf("columns must list 1 to %d columns", MaxViewTableCols)
		}
		if len(b.Cells) > MaxViewTableRows {
			return Block{}, fmt.Errorf("at most %d rows", MaxViewTableRows)
		}
		for _, c := range b.Columns {
			if err := viewText("column", c, MaxViewLabelLen, true); err != nil {
				return Block{}, err
			}
		}
		for _, row := range b.Cells {
			if len(row) != len(b.Columns) {
				return Block{}, fmt.Errorf("every row needs %d cells", len(b.Columns))
			}
			for _, c := range row {
				if err := viewText("cell", c, MaxViewCellLen, false); err != nil {
					return Block{}, err
				}
			}
		}
	case BlockText:
		if b.Text == "" || len(b.Text) > MaxViewTextLen || !utf8.ValidString(b.Text) || hasHiddenRunes(strings.ReplaceAll(b.Text, "\n", "")) {
			return Block{}, fmt.Errorf("text must be 1 to %d bytes of plain text (newlines allowed)", MaxViewTextLen)
		}
	}
	return b, nil
}

func checkItems(items []ViewItem, maxItems int, stats bool) error {
	if len(items) == 0 || len(items) > maxItems {
		return fmt.Errorf("items must list 1 to %d items", maxItems)
	}
	tones := set(Tones...)
	for _, it := range items {
		if err := viewText("label", it.Label, MaxViewLabelLen, true); err != nil {
			return err
		}
		if err := viewText("note", it.Note, MaxViewLabelLen, false); err != nil {
			return err
		}
		if it.Tone != "" && !tones[it.Tone] {
			return fmt.Errorf("tone %q is not one of %s", it.Tone, strings.Join(Tones, ", "))
		}
		if stats {
			if err := viewText("value", it.Value, MaxViewValueLen, true); err != nil {
				return err
			}
			if it.Count != 0 {
				return errors.New("stats items take value, not count")
			}
		} else {
			if it.Value != "" {
				return errors.New("bars items take count, not value")
			}
			if err := checkValues([]float64{it.Count}, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkValues(values []float64, maxValues int) error {
	if len(values) > maxValues {
		return fmt.Errorf("at most %d values", maxValues)
	}
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return errors.New("values must be finite and not negative")
		}
	}
	return nil
}

func viewText(field, s string, maxRunes int, required bool) error {
	switch {
	case required && strings.TrimSpace(s) == "":
		return fmt.Errorf("%s is required", field)
	case utf8.RuneCountInString(s) > maxRunes:
		return fmt.Errorf("%s longer than %d characters", field, maxRunes)
	case !utf8.ValidString(s) || hasHiddenRunes(s):
		return fmt.Errorf("%s must be plain text", field)
	}
	return nil
}
