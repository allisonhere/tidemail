package richmail

import (
	"strings"
	"sync"
)

// Part is one non-text MIME part available for embedded-image resolution.
// It intentionally mirrors the fields of db.Attachment without importing the
// database package, so the resolver stays independently testable.
type Part struct {
	ContentID       string
	ContentLocation string
	ContentType     string
	Data            []byte
}

// PartStore indexes a message's parts for CID and Content-Location lookup.
// It is safe for concurrent reads.
type PartStore struct {
	mu         sync.RWMutex
	byCID      map[string][]byte
	byLocation map[string][]byte
}

// NewPartStore builds an index from one message's parts. Entries are keyed by
// case-folded CID and by raw Content-Location; a duplicate CID keeps its first
// part, matching the way mail clients resolve ambiguous references.
func NewPartStore(parts []Part) *PartStore {
	s := &PartStore{
		byCID:      make(map[string][]byte, len(parts)),
		byLocation: make(map[string][]byte, len(parts)),
	}
	for _, p := range parts {
		if key := CIDKey(p.ContentID); key != "" {
			if _, exists := s.byCID[key]; !exists {
				s.byCID[key] = p.Data
			}
		}
		if loc := normalizeLocationKey(p.ContentLocation); loc != "" {
			if _, exists := s.byLocation[loc]; !exists {
				s.byLocation[loc] = p.Data
			}
		}
	}
	return s
}

// Len reports how many addressable parts the store holds.
func (s *PartStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byCID) + len(s.byLocation)
}

func (s *PartStore) lookupCID(raw string) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.byCID[CIDKey(raw)]
	return data, ok
}

func (s *PartStore) lookupLocation(raw string) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.byLocation[normalizeLocationKey(raw)]
	return data, ok
}

func normalizeLocationKey(raw string) string {
	s := strings.TrimSpace(stripControl(raw))
	if strings.HasPrefix(strings.ToLower(s), "cid:") {
		s = s[len("cid:"):]
	}
	return strings.TrimSpace(s)
}

// LocationKey exposes the Content-Location/bare-filename normalizer so the UI
// can match rendered images back to their attachment rows.
func LocationKey(raw string) string { return normalizeLocationKey(raw) }

// ResolveStatus explains why an image did or did not resolve.
type ResolveStatus int

const (
	// ResolveOK means Decoded is usable.
	ResolveOK ResolveStatus = iota
	// ResolveEmbeddedMissing means a cid:/location reference had no matching part.
	ResolveEmbeddedMissing
	// ResolveUnsupported means TideMail will never load this source (file:, etc.).
	ResolveUnsupported
	// ResolveTooLarge means the image exceeded size or dimension limits.
	ResolveTooLarge
	// ResolveMalformed means the bytes were not a decodable image.
	ResolveMalformed
	// ResolveRemoteBlocked means an http(s) source awaits user consent.
	ResolveRemoteBlocked
	// ResolveDeferred means the source needs asynchronous work (remote fetch)
	// that has not completed yet.
	ResolveDeferred
)

// Resolved is the outcome of resolving one image.
type Resolved struct {
	Status  ResolveStatus
	Decoded *Decoded
	// Reason is a short human-readable explanation for non-OK statuses.
	Reason string
}

// ResolveEmbedded resolves locally-available sources: CID references against
// the message's parts, Content-Location references, and data URIs. Remote URLs
// are reported as ResolveRemoteBlocked and must be fetched by the caller under
// its privacy policy.
func ResolveEmbedded(im Image, store *PartStore, limits Limits) Resolved {
	src := im.Source
	switch src.Kind {
	case SourceData:
		return decodeBytes(src.Data, limits)

	case SourceCID:
		if data, ok := store.lookupCID(src.CID); ok {
			return decodeBytes(data, limits)
		}
		// Some senders point cid: at a part that only carries Content-Location.
		if data, ok := store.lookupLocation(src.CID); ok {
			return decodeBytes(data, limits)
		}
		return Resolved{Status: ResolveEmbeddedMissing, Reason: "embedded image not found"}

	case SourceUnknown:
		// A relative or bare filename may match a part's Content-Location.
		if data, ok := store.lookupLocation(src.Raw); ok {
			return decodeBytes(data, limits)
		}
		return Resolved{Status: ResolveUnsupported, Reason: "unsupported image source"}

	case SourceRemote:
		return Resolved{Status: ResolveRemoteBlocked, Reason: "remote image blocked"}

	default:
		return Resolved{Status: ResolveUnsupported, Reason: "unsupported image source"}
	}
}

// DecodeRemote decodes bytes that a remote fetch already retrieved.
func DecodeRemote(data []byte, limits Limits) Resolved {
	return decodeBytes(data, limits)
}

func decodeBytes(data []byte, limits Limits) Resolved {
	decoded, err := Decode(data, limits)
	if err != nil {
		switch err {
		case ErrImageTooLarge, ErrImageTooWide:
			return Resolved{Status: ResolveTooLarge, Reason: "image too large"}
		default:
			return Resolved{Status: ResolveMalformed, Reason: "unreadable image"}
		}
	}
	return Resolved{Status: ResolveOK, Decoded: decoded}
}
