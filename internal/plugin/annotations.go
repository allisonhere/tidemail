package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"unicode"
	"unicode/utf8"
)

// Limits on annotations returned by one message.metadata call.
const (
	MaxAnnotations        = 32
	MaxAnnotationKeyLen   = 64
	MaxAnnotationValueLen = 512 // bytes
)

// annotationKeyPattern is deliberately conservative: keys are identifiers,
// never display text, so they cannot carry escapes or look-alike characters.
var annotationKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

// Annotation is one key/value a plugin attaches to a message. TideMail stores
// and displays annotations but never acts on them.
type Annotation struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Confidence is optional; nil means the plugin did not supply one.
	Confidence *float64 `json:"confidence,omitempty"`
}

// messageMetadataResponse is the part of a message.metadata response TideMail
// interprets. Every other field of the data is display-only.
type messageMetadataResponse struct {
	Annotations json.RawMessage `json:"annotations"`
}

// ParseAnnotations extracts and validates the annotations in a
// message.metadata response. Missing or null annotations are an empty set.
// The set is all-or-nothing: one invalid entry rejects the whole response, so
// a bad reply can never partly overwrite stored annotations.
func ParseAnnotations(data json.RawMessage) ([]Annotation, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] != '{' {
		return nil, errors.New("response data is not a JSON object")
	}
	var resp messageMetadataResponse
	if err := json.Unmarshal(trimmed, &resp); err != nil {
		return nil, fmt.Errorf("malformed response data: %w", err)
	}
	raw := bytes.TrimSpace(resp.Annotations)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, errors.New("annotations must be an array")
	}
	if len(entries) > MaxAnnotations {
		return nil, fmt.Errorf("too many annotations: %d (limit %d)", len(entries), MaxAnnotations)
	}
	out := make([]Annotation, 0, len(entries))
	seen := map[string]bool{}
	for i, entry := range entries {
		var a Annotation
		if err := json.Unmarshal(entry, &a); err != nil {
			return nil, fmt.Errorf("annotation %d: must be an object with string key and value", i+1)
		}
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("annotation %d: %w", i+1, err)
		}
		if seen[a.Key] {
			return nil, fmt.Errorf("annotation %d: duplicate key %q", i+1, a.Key)
		}
		seen[a.Key] = true
		out = append(out, a)
	}
	return out, nil
}

// Validate checks one annotation against the protocol limits.
func (a Annotation) Validate() error {
	switch {
	case a.Key == "":
		return errors.New("key is required")
	case len(a.Key) > MaxAnnotationKeyLen:
		return fmt.Errorf("key longer than %d characters", MaxAnnotationKeyLen)
	case !annotationKeyPattern.MatchString(a.Key):
		return fmt.Errorf("key %q must use a-z, 0-9, '_', '.' or '-', starting with a letter or digit", a.Key)
	case len(a.Value) > MaxAnnotationValueLen:
		return fmt.Errorf("value for %q longer than %d bytes", a.Key, MaxAnnotationValueLen)
	case !utf8.ValidString(a.Value):
		return fmt.Errorf("value for %q is not valid UTF-8", a.Key)
	case hasHiddenRunes(a.Value):
		return fmt.Errorf("value for %q contains control or invisible characters", a.Key)
	}
	if c := a.Confidence; c != nil && (math.IsNaN(*c) || *c < 0 || *c > 1) {
		return fmt.Errorf("confidence for %q must be between 0 and 1", a.Key)
	}
	return nil
}

// hasHiddenRunes reports control characters (including escapes and
// newlines), format characters (bidi overrides, zero-width), and line or
// paragraph separators.
func hasHiddenRunes(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return true
		}
	}
	return false
}

// AnnotationStore persists validated annotations. ReplaceAnnotations must make
// anns the plugin's complete set for the message, atomically.
type AnnotationStore interface {
	ReplaceAnnotations(pluginID string, messageID int64, anns []Annotation) error
}

// AnnotationOutcome says what happened to a response's annotations.
type AnnotationOutcome int

const (
	// AnnotationsNotPermitted: the plugin lacks the annotations permission, so
	// nothing was stored. The response is still shown.
	AnnotationsNotPermitted AnnotationOutcome = iota
	// AnnotationsStored: the validated set (possibly empty) replaced the
	// plugin's previous annotations on the message.
	AnnotationsStored
	// AnnotationsRejected: the annotations failed validation; stored
	// annotations are unchanged.
	AnnotationsRejected
	// AnnotationsNotStored: the store was unavailable or failed; stored
	// annotations are unchanged.
	AnnotationsNotStored
)

// MessageMetadataResult is a successful message.metadata run.
type MessageMetadataResult struct {
	Response Response
	// Annotations is the validated set from the response, whether or not it
	// was stored. It is nil when the response was rejected.
	Annotations []Annotation
	Outcome     AnnotationOutcome
	// AnnotationErr explains AnnotationsRejected and AnnotationsNotStored.
	AnnotationErr error
	// Presentation is the plugin's optional human-facing explanation.
	// PresentationErr says why a presentation that was sent was ignored;
	// it never affects the annotations.
	Presentation    *Presentation
	PresentationErr error
}
