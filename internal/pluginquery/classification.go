package pluginquery

import (
	"github.com/allisonhere/tidemail/internal/attention"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// AnnotationRow is one stored plugin annotation: a plugin's judgment, never
// a user correction.
type AnnotationRow struct {
	MessageID  int64    `json:"message_id"`
	PluginID   string   `json:"plugin_id"`
	Key        string   `json:"key"`
	Value      string   `json:"value"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// AnnotationsResult is query.annotations' result.
type AnnotationsResult struct {
	Annotations []AnnotationRow `json:"annotations"`
}

// existingIDs keeps the requested IDs that are cached messages.
func (e *Executor) existingIDs(ids []int64) ([]int64, error) {
	rows, err := e.DB.ListHeaders(db.HeaderQuery{IDs: ids})
	if err != nil {
		return nil, err
	}
	known := make(map[int64]bool, len(rows))
	for _, m := range rows {
		known[m.ID] = true
	}
	out := make([]int64, 0, len(rows))
	for _, id := range ids {
		if known[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (e *Executor) annotations(q plugin.Query) (AnnotationsResult, error) {
	ids, err := e.existingIDs(q.MessageIDs)
	if err != nil {
		return AnnotationsResult{}, err
	}
	stored, err := e.DB.ListPluginAnnotationsForMessages(ids)
	if err != nil {
		return AnnotationsResult{}, err
	}
	plugins, keys := stringSet(q.PluginIDs), stringSet(q.Keys)
	out := AnnotationsResult{Annotations: []AnnotationRow{}}
	for _, id := range ids {
		for _, a := range stored[id] {
			if (plugins != nil && !plugins[a.PluginID]) || (keys != nil && !keys[a.Key]) {
				continue
			}
			out.Annotations = append(out.Annotations, AnnotationRow{
				MessageID: a.MessageID, PluginID: a.PluginID, Key: a.Key, Value: clean(a.Value), Confidence: a.Confidence,
			})
		}
	}
	return out, nil
}

// Signal is one effective classification field with its provenance.
type Signal struct {
	Value  any    `json:"value"`
	Source string `json:"source"` // "plugin", "user", or "none"
}

// ClassificationRow is a message's effective classification: what TideMail
// shows and acts on, after user corrections.
type ClassificationRow struct {
	MessageID  int64  `json:"message_id"`
	NeedsReply Signal `json:"needs_reply"`
	Urgent     Signal `json:"urgent"`
	Important  Signal `json:"important"`
	Category   Signal `json:"category"`
}

// ClassificationResult is query.classification's result.
type ClassificationResult struct {
	Classifications []ClassificationRow `json:"classifications"`
}

func (e *Executor) classification(q plugin.Query) (ClassificationResult, error) {
	ids, err := e.existingIDs(q.MessageIDs)
	if err != nil {
		return ClassificationResult{}, err
	}
	effective, err := e.effective(ids)
	if err != nil {
		return ClassificationResult{}, err
	}
	out := ClassificationResult{Classifications: []ClassificationRow{}}
	for _, id := range ids {
		c := effective[id]
		out.Classifications = append(out.Classifications, ClassificationRow{
			MessageID:  id,
			NeedsReply: Signal{c.NeedsReply, source(c.NeedsReplySource)},
			Urgent:     Signal{c.Urgent, source(c.UrgencySource)},
			Important:  Signal{c.Important, source(c.ImportanceSource)},
			Category:   Signal{clean(c.Category), source(c.CategorySource)},
		})
	}
	return out, nil
}

// effective computes TideMail's effective classification (plugin
// annotations with user corrections applied) for ids, in two batched reads.
func (e *Executor) effective(ids []int64) (map[int64]attention.EffectiveClassification, error) {
	anns, err := e.DB.ListPluginAnnotationsForMessages(ids)
	if err != nil {
		return nil, err
	}
	overrides, err := e.DB.GetClassificationOverridesForMessages(ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]attention.EffectiveClassification, len(ids))
	for _, id := range ids {
		out[id] = attention.FromDB(anns[id], overrides[id])
	}
	return out, nil
}

func source(s attention.Source) string {
	if s == "" {
		return string(attention.SourceNone)
	}
	return string(s)
}

func stringSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}
