package attention

import (
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
)

type Source string

const (
	SourceNone   Source = "none"
	SourcePlugin Source = "plugin"
	SourceUser   Source = "user"
)

type Annotation struct{ Key, Value string }

type Override struct{ Key, Value string }

type EffectiveClassification struct {
	NeedsReply, Urgent, Important                                     bool
	Category                                                          string
	NeedsReplySource, UrgencySource, ImportanceSource, CategorySource Source
}

func (c EffectiveClassification) Any() bool { return c.NeedsReply || c.Urgent || c.Important }

var (
	needsReplyValues = map[string]bool{"true": true, "yes": true, "1": true}
	urgentValues     = map[string]bool{"high": true, "urgent": true, "critical": true}
	importantValues  = map[string]bool{"high": true}
)

func FromDB(annotations []db.PluginAnnotation, overrides map[string]db.ClassificationOverride) EffectiveClassification {
	plain := make([]Annotation, 0, len(annotations))
	for _, a := range annotations {
		plain = append(plain, Annotation{Key: a.Key, Value: a.Value})
	}
	converted := map[string]Override{}
	for k, v := range overrides {
		converted[k] = Override{Key: k, Value: v.Value}
	}
	return Compute(plain, converted)
}

func Compute(annotations []Annotation, overrides map[string]Override) EffectiveClassification {
	var out EffectiveClassification
	plugin := map[string][]string{}
	for _, a := range annotations {
		plugin[a.Key] = append(plugin[a.Key], strings.ToLower(strings.TrimSpace(a.Value)))
	}
	value := func(key string) (string, Source) {
		if o, ok := overrides[key]; ok && strings.ToLower(strings.TrimSpace(o.Value)) != db.ClassificationPlugin {
			return strings.ToLower(strings.TrimSpace(o.Value)), SourceUser
		}
		return "", SourcePlugin
	}
	if v, source := value(db.ClassificationNeedsReply); source == SourceUser {
		out.NeedsReply, out.NeedsReplySource = v == "true", source
	} else {
		for _, v := range plugin[db.ClassificationNeedsReply] {
			if needsReplyValues[v] {
				out.NeedsReply = true
				break
			}
		}
		if out.NeedsReply {
			out.NeedsReplySource = SourcePlugin
		}
	}
	if v, source := value(db.ClassificationUrgency); source == SourceUser {
		out.Urgent, out.UrgencySource = v == "high", source
	} else {
		for _, v := range plugin[db.ClassificationUrgency] {
			if urgentValues[v] {
				out.Urgent = true
				break
			}
		}
		if out.Urgent {
			out.UrgencySource = SourcePlugin
		}
	}
	if v, source := value(db.ClassificationImportance); source == SourceUser {
		out.Important, out.ImportanceSource = v == "high", source
	} else {
		for _, v := range plugin[db.ClassificationImportance] {
			if importantValues[v] {
				out.Important = true
				break
			}
		}
		if out.Important {
			out.ImportanceSource = SourcePlugin
		}
	}
	if v, source := value(db.ClassificationCategory); source == SourceUser {
		if v != db.ClassificationNone {
			out.Category = v
		}
		out.CategorySource = source
	} else {
		for _, v := range plugin[db.ClassificationCategory] {
			if v != "" {
				out.Category = v
				out.CategorySource = SourcePlugin
				break
			}
		}
	}
	return out
}
