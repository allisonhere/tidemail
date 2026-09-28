package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Presentation is an optional, human-facing explanation a plugin can return
// alongside its annotations in message.metadata / message.received data:
//
//	annotations  → for machines: stored, searched, used by views
//	presentation → for people: shown once, in the result card
//
// It carries meaning only (words, a status, a confidence level); TideMail
// chooses layout, colors, and keys. A malformed presentation never costs the
// run its annotations: it is dropped with a warning.
type Presentation struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary,omitempty"`
	Status     string   `json:"status,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
	Facts      []Fact   `json:"facts,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
}

// Fact is one label/value row.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Presentation statuses; TideMail maps them to theme colors.
const (
	StatusInfo    = "info"
	StatusSuccess = "success"
	StatusWarning = "warning"
	StatusDanger  = "danger"
	StatusNeutral = "neutral"
)

// PresentationStatuses lists every status.
var PresentationStatuses = []string{StatusInfo, StatusSuccess, StatusWarning, StatusDanger, StatusNeutral}

// PresentationConfidences are the confidence levels a plugin may state.
var PresentationConfidences = []string{"high", "medium", "low"}

// Presentation limits (characters, except where noted).
const (
	MaxPresentationTitle   = 100
	MaxPresentationSummary = 500
	MaxPresentationFacts   = 8
	MaxFactLabel           = 40
	MaxFactValue           = 160
	MaxPresentationReasons = 6
	MaxPresentationReason  = 180
)

// ConfidenceLabel maps an annotation confidence to the words people see:
// High (>= 0.85), Medium (>= 0.65), Low otherwise.
func ConfidenceLabel(c float64) string {
	switch {
	case c >= 0.85:
		return "High"
	case c >= 0.65:
		return "Medium"
	}
	return "Low"
}

// presentationEnvelope is the part of a metadata response that carries it.
type presentationEnvelope struct {
	Presentation json.RawMessage `json:"presentation"`
}

// ExtractPresentation reads the optional presentation from a metadata
// response's data. It returns nil and no error when there is none, and an
// error (for a warning) when one is present but invalid.
func ExtractPresentation(data json.RawMessage) (*Presentation, error) {
	var env presentationEnvelope
	if len(data) == 0 || json.Unmarshal(data, &env) != nil || len(env.Presentation) == 0 || isJSONNull(env.Presentation) {
		return nil, nil
	}
	p, err := ParsePresentation(env.Presentation)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ParsePresentation validates a presentation strictly: unknown fields,
// statuses, or confidence words, oversized text, and any control, format,
// or escape character are errors.
func ParsePresentation(raw json.RawMessage) (Presentation, error) {
	var p Presentation
	if err := strictDecode(raw, &p); err != nil {
		return Presentation{}, fmt.Errorf("presentation: %w", err)
	}
	if err := presentationText("title", p.Title, MaxPresentationTitle, true); err != nil {
		return Presentation{}, err
	}
	if err := presentationText("summary", p.Summary, MaxPresentationSummary, false); err != nil {
		return Presentation{}, err
	}
	if p.Status != "" && !set(PresentationStatuses...)[p.Status] {
		return Presentation{}, fmt.Errorf("presentation status %q is not one of %s", p.Status, strings.Join(PresentationStatuses, ", "))
	}
	if p.Confidence != "" && !set(PresentationConfidences...)[p.Confidence] {
		return Presentation{}, fmt.Errorf("presentation confidence %q is not one of %s", p.Confidence, strings.Join(PresentationConfidences, ", "))
	}
	if len(p.Facts) > MaxPresentationFacts {
		return Presentation{}, fmt.Errorf("presentation has %d facts (at most %d)", len(p.Facts), MaxPresentationFacts)
	}
	for _, f := range p.Facts {
		if err := presentationText("fact label", f.Label, MaxFactLabel, true); err != nil {
			return Presentation{}, err
		}
		if err := presentationText("fact value", f.Value, MaxFactValue, true); err != nil {
			return Presentation{}, err
		}
	}
	if len(p.Reasons) > MaxPresentationReasons {
		return Presentation{}, fmt.Errorf("presentation has %d reasons (at most %d)", len(p.Reasons), MaxPresentationReasons)
	}
	for _, r := range p.Reasons {
		if err := presentationText("reason", r, MaxPresentationReason, true); err != nil {
			return Presentation{}, err
		}
	}
	return p, nil
}

func presentationText(field, s string, maxRunes int, required bool) error {
	switch {
	case !utf8.ValidString(s):
		return fmt.Errorf("presentation %s is not valid UTF-8", field)
	case hasHiddenRunes(s):
		return fmt.Errorf("presentation %s contains control or escape characters", field)
	case required && strings.TrimSpace(s) == "":
		return fmt.Errorf("presentation %s is required", field)
	case utf8.RuneCountInString(s) > maxRunes:
		return fmt.Errorf("presentation %s longer than %d characters", field, maxRunes)
	}
	return nil
}
