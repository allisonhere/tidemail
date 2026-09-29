package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// recordingStore is an AnnotationStore that records every write.
type recordingStore struct {
	calls []storeCall
	err   error
}

type storeCall struct {
	pluginID  string
	messageID int64
	anns      []Annotation
}

func (s *recordingStore) ReplaceAnnotations(pluginID string, messageID int64, anns []Annotation) error {
	s.calls = append(s.calls, storeCall{pluginID, messageID, anns})
	return s.err
}

func TestParseAnnotationsValid(t *testing.T) {
	anns, err := ParseAnnotations(json.RawMessage(`{
		"summary": "ignored",
		"actions": [{"move": "Trash"}],
		"annotations": [
			{"key": "needs_reply", "value": "true", "confidence": 0.94, "extra": "ignored"},
			{"key": "category.v2", "value": "receipt", "confidence": 0},
			{"key": "urgency", "value": ""}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 3 {
		t.Fatalf("anns = %+v", anns)
	}
	if anns[0].Key != "needs_reply" || anns[0].Value != "true" || anns[0].Confidence == nil || *anns[0].Confidence != 0.94 {
		t.Fatalf("first = %+v", anns[0])
	}
	// Zero confidence is distinct from none.
	if anns[1].Confidence == nil || *anns[1].Confidence != 0 {
		t.Fatalf("zero confidence lost: %+v", anns[1])
	}
	if anns[2].Confidence != nil {
		t.Fatalf("missing confidence should be nil: %+v", anns[2])
	}
}

func TestParseAnnotationsEmptyForms(t *testing.T) {
	for _, data := range []string{``, `null`, `{}`, `{"summary":"x"}`, `{"annotations":null}`, `{"annotations":[]}`} {
		anns, err := ParseAnnotations(json.RawMessage(data))
		if err != nil || len(anns) != 0 {
			t.Errorf("%q: anns=%v err=%v", data, anns, err)
		}
	}
}

func TestParseAnnotationsRejects(t *testing.T) {
	tooMany := make([]string, MaxAnnotations+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf(`{"key":"k%d","value":"v"}`, i)
	}
	tests := []struct {
		name, data, want string
	}{
		{"empty key", `{"annotations":[{"key":"","value":"x"}]}`, "key is required"},
		{"missing key", `{"annotations":[{"value":"x"}]}`, "key is required"},
		{"uppercase key", `{"annotations":[{"key":"Urgency","value":"x"}]}`, "must use a-z"},
		{"spaces in key", `{"annotations":[{"key":"needs reply","value":"x"}]}`, "must use a-z"},
		{"leading dot", `{"annotations":[{"key":".x","value":"x"}]}`, "must use a-z"},
		{"escape in key", `{"annotations":[{"key":"a\u001b[31m","value":"x"}]}`, "must use a-z"},
		{"long key", `{"annotations":[{"key":"` + strings.Repeat("k", MaxAnnotationKeyLen+1) + `","value":"x"}]}`, "key longer than 64"},
		{"long value", `{"annotations":[{"key":"k","value":"` + strings.Repeat("v", MaxAnnotationValueLen+1) + `"}]}`, "longer than 512 bytes"},
		{"negative confidence", `{"annotations":[{"key":"k","value":"v","confidence":-0.01}]}`, "between 0 and 1"},
		{"confidence above one", `{"annotations":[{"key":"urgency","value":"high","confidence":2.0}]}`, "between 0 and 1"},
		{"too many", `{"annotations":[` + strings.Join(tooMany, ",") + `]}`, "too many annotations: 33"},
		{"escape in value", `{"annotations":[{"key":"k","value":"a\u001b[31mb"}]}`, "control or invisible"},
		{"newline in value", `{"annotations":[{"key":"k","value":"a\nb"}]}`, "control or invisible"},
		{"bidi override", `{"annotations":[{"key":"k","value":"a\u202eb"}]}`, "control or invisible"},
		{"zero width", `{"annotations":[{"key":"k","value":"a\u200bb"}]}`, "control or invisible"},
		{"line separator", `{"annotations":[{"key":"k","value":"a\u2028b"}]}`, "control or invisible"},
		{"duplicate key", `{"annotations":[{"key":"k","value":"a"},{"key":"k","value":"b"}]}`, "duplicate key"},
		{"non-string value", `{"annotations":[{"key":"k","value":5}]}`, "must be an object"},
		{"not an array", `{"annotations":{"key":"k"}}`, "must be an array"},
		{"data not an object", `["annotations"]`, "not a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anns, err := ParseAnnotations(json.RawMessage(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			if anns != nil {
				t.Fatalf("a rejected set must return no annotations, got %v", anns)
			}
		})
	}
}

// One bad entry rejects the whole set, even when the others are valid.
func TestParseAnnotationsAllOrNothing(t *testing.T) {
	anns, err := ParseAnnotations(json.RawMessage(`{"annotations":[{"key":"ok","value":"1"},{"key":"BAD","value":"2"}]}`))
	if err == nil || anns != nil {
		t.Fatalf("anns=%v err=%v", anns, err)
	}
}

func discoverWith(t *testing.T, command, permissions string) *Manager {
	t.Helper()
	root := t.TempDir()
	installPlugin(t, root, "p", "p", command, permissions)
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Plugin("p"); !ok {
		t.Fatalf("plugin not discovered: %v", m.Errors())
	}
	return m
}

const bothPermissions = "[permissions]\nmessage_metadata = true\nannotations = true\n"

func TestMessageMetadataStoresPermittedAnnotations(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-annotate", bothPermissions)
	store := &recordingStore{}
	result, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != AnnotationsStored || result.AnnotationErr != nil {
		t.Fatalf("outcome = %v, err = %v", result.Outcome, result.AnnotationErr)
	}
	if len(store.calls) != 1 {
		t.Fatalf("store calls = %d", len(store.calls))
	}
	call := store.calls[0]
	if call.pluginID != "p" || call.messageID != 42 || len(call.anns) != 2 || call.anns[1].Key != "urgency" {
		t.Fatalf("store call = %+v", call)
	}
	// The display data is still there for the UI.
	if !strings.Contains(string(result.Response.Data), "looks like a notification") {
		t.Fatalf("data = %s", result.Response.Data)
	}
}

func TestMessageMetadataWithoutAnnotationPermissionStoresNothing(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-annotate", metadataPermission)
	store := &recordingStore{}
	result, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != AnnotationsNotPermitted || len(store.calls) != 0 {
		t.Fatalf("outcome = %v, store calls = %d", result.Outcome, len(store.calls))
	}
	// The response itself is still returned for display.
	if len(result.Annotations) != 2 || len(result.Response.Data) == 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAnnotationPermissionAloneCannotRunMetadata(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-annotate", "[permissions]\nannotations = true\n")
	store := &recordingStore{}
	if _, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if len(store.calls) != 0 {
		t.Fatal("store written without message_metadata")
	}
}

func TestRejectedAnnotationsLeaveStoreUntouched(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-badconf", bothPermissions)
	store := &recordingStore{}
	result, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != AnnotationsRejected || result.AnnotationErr == nil || len(store.calls) != 0 {
		t.Fatalf("outcome = %v err = %v calls = %d", result.Outcome, result.AnnotationErr, len(store.calls))
	}
}

func TestEmptyAnnotationsReplaceWithEmptySet(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-noanns", bothPermissions)
	store := &recordingStore{}
	result, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != AnnotationsStored || len(store.calls) != 1 || len(store.calls[0].anns) != 0 {
		t.Fatalf("outcome = %v calls = %+v", result.Outcome, store.calls)
	}
}

func TestFailedRunNeverTouchesStore(t *testing.T) {
	for _, command := range []string{"tidemail-plugin-exit1", "tidemail-plugin-badjson", "tidemail-plugin-wrongid", "tidemail-plugin-errresp"} {
		t.Run(command, func(t *testing.T) {
			m := discoverWith(t, command, bothPermissions)
			store := &recordingStore{}
			if _, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store); err == nil {
				t.Fatal("expected the run to fail")
			}
			if len(store.calls) != 0 {
				t.Fatal("store written after a failed run")
			}
		})
	}
}

func TestStoreFailureIsReportedNotFatal(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-annotate", bothPermissions)
	store := &recordingStore{err: errors.New("disk full")}
	result, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != AnnotationsNotStored || result.AnnotationErr == nil {
		t.Fatalf("outcome = %v err = %v", result.Outcome, result.AnnotationErr)
	}
	// A nil store is also reported, not a crash.
	result, err = m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 42}, nil)
	if err != nil || result.Outcome != AnnotationsNotStored {
		t.Fatalf("nil store: outcome = %v err = %v", result.Outcome, err)
	}
}

// Call is the only other way to reach a plugin with a message method, and it
// refuses; ping responses are never parsed for annotations.
func TestNoAlternatePathToStore(t *testing.T) {
	m := discoverWith(t, "tidemail-plugin-annotate", bothPermissions)
	if _, err := m.Call(context.Background(), "p", MethodMessageMetadata, json.RawMessage(`{"id":42}`)); err == nil {
		t.Fatal("Call should refuse message.metadata")
	}
}
