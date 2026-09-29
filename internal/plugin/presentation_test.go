package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func init() {
	helpers["tidemail-plugin-present"] = helperAnnotateWith(`{"annotations":[{"key":"category","value":"newsletter","confidence":0.85}],
		"presentation":{"title":"This looks like a newsletter","summary":"Automated list mail.","status":"info","confidence":"high",
		"facts":[{"label":"Type","value":"Newsletter"}],"reasons":["Matches newsletter patterns"]}}`)
	helpers["tidemail-plugin-badpresent"] = helperAnnotateWith(`{"annotations":[{"key":"category","value":"newsletter"}],
		"presentation":{"title":"\u001b[31mRED","status":"info"}}`)
}

// helperAnnotateWith answers metadata calls with a fixed data object.
func helperAnnotateWith(data string) func() {
	return func() {
		req := readRequest()
		respond(req, json.RawMessage(data))
	}
}

const validPresentation = `{"title":"This looks like a newsletter","summary":"Automated list mail with low expected priority.",
 "status":"info","confidence":"high","facts":[{"label":"Type","value":"Newsletter"},{"label":"Priority","value":"Low"}],
 "reasons":["Matches newsletter/list-mail patterns","Sent by an automated mailing source"]}`

func TestParsePresentationValid(t *testing.T) {
	p, err := ParsePresentation(json.RawMessage(validPresentation))
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "This looks like a newsletter" || p.Summary == "" || p.Status != StatusInfo || p.Confidence != "high" ||
		len(p.Facts) != 2 || p.Facts[1].Value != "Low" || len(p.Reasons) != 2 {
		t.Fatalf("presentation = %+v", p)
	}
}

func TestParsePresentationRejects(t *testing.T) {
	facts := make([]string, MaxPresentationFacts+1)
	for i := range facts {
		facts[i] = `{"label":"a","value":"b"}`
	}
	cases := map[string]string{
		`{"title":"x","status":"purple"}`:                                                  "status",
		`{"title":"x","confidence":"0.85"}`:                                                "confidence",
		`{"title":"` + strings.Repeat("a", 101) + `"}`:                                     "title longer",
		`{"title":"x","summary":"` + strings.Repeat("a", 501) + `"}`:                       "summary longer",
		`{"title":"x","facts":[` + strings.Join(facts, ",") + `]}`:                         "facts",
		`{"title":"x","facts":[{"label":"a","value":"` + strings.Repeat("v", 161) + `"}]}`: "fact value longer",
		`{"title":"x","reasons":["a","b","c","d","e","f","g"]}`:                            "reasons",
		`{"title":"x\u0007"}`:                                                              "control",
		`{"title":"x","reasons":["\u001b[31mred"]}`:                                        "control",
		`{"title":"x","summary":"\u202eevil"}`:                                             "control",
		`{"title":""}`:                                                                     "title is required",
		`{"title":"x","color":"red"}`:                                                      "unknown field",
	}
	for raw, want := range cases {
		if _, err := ParsePresentation(json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.60s: err = %v, want %q", raw, err, want)
		}
	}
}

func TestConfidenceLabel(t *testing.T) {
	for c, want := range map[float64]string{0.98: "High", 0.85: "High", 0.84: "Medium", 0.65: "Medium", 0.64: "Low", 0: "Low"} {
		if got := ConfidenceLabel(c); got != want {
			t.Errorf("ConfidenceLabel(%v) = %q, want %q", c, got, want)
		}
	}
}

func TestPresentationTravelsWithAnnotations(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "p", "p", "tidemail-plugin-present", "[permissions]\nmessage_metadata = true\nannotations = true")
	m, _ := Discover(root)
	store := &recordingStore{}
	res, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != AnnotationsStored || res.Presentation == nil || res.Presentation.Title != "This looks like a newsletter" || res.PresentationErr != nil {
		t.Fatalf("result = %+v", res)
	}
}

func TestMalformedPresentationKeepsAnnotations(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "p", "p", "tidemail-plugin-badpresent", "[permissions]\nmessage_metadata = true\nannotations = true")
	m, _ := Discover(root)
	store := &recordingStore{}
	res, err := m.MessageMetadata(context.Background(), "p", MessageMetadata{ID: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != AnnotationsStored || len(res.Annotations) != 1 {
		t.Fatalf("annotations lost: %+v", res)
	}
	if res.Presentation != nil || res.PresentationErr == nil {
		t.Fatalf("bad presentation should be dropped with a warning: %+v", res)
	}
}

func TestManifestDescriptionAndHomepage(t *testing.T) {
	base := "id = \"a\"\nname = \"A\"\napi = 1\ncommand = \"run\"\n"
	if m, err := ParseManifest([]byte(base + "description = \"Detect newsletters\"\nhomepage = \"https://example.com/a\"\n")); err != nil || m.Description != "Detect newsletters" {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
	for _, bad := range []string{
		"description = \"" + strings.Repeat("d", 121) + "\"\n",
		"description = \"a\\u001b[31m\"\n",
		"homepage = \"http://example.com\"\n",
		"homepage = \"javascript:alert(1)\"\n",
	} {
		if _, err := ParseManifest([]byte(base + bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
