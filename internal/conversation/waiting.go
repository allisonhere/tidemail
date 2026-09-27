package conversation

import (
	"sort"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

// Waiting on Them: conversations where the user sent the latest meaningful
// message to someone else and no reply has arrived yet. It is conversation
// state only: no plugin, annotation, or network call is involved.

// WaitingThread is one conversation waiting on someone else.
type WaitingThread struct {
	// Message is the user's latest message.
	Message db.Message
	// Key identifies this waiting cycle (db.WaitingKey of Message).
	Key string
	// Since is when the user sent that message.
	Since time.Time
	// WaitingFor are the other real people it was sent to.
	WaitingFor []Participant
}

// ComputeWaiting finds waiting conversations. A thread qualifies when its
// newest meaningful message is from the user and was sent to at least one
// real person who is not the user. Stopped and snoozed cycles are left out;
// stopped maps the row ID of a stopped cycle's message to its key. stale lists
// snoozed keys whose cycle has ended (someone replied, or the user wrote
// again). The oldest wait comes first.
func ComputeWaiting(candidates []db.Message, me, stoppedKeys, snoozedKeys map[string]bool) (waiting []WaitingThread, stopped map[int64]string, stale []string) {
	stopped = map[int64]string{}
	if len(me) == 0 {
		return nil, stopped, nil
	}
	for _, thread := range BuildThreads(candidates) {
		var latest *db.Message
		for i := len(thread.Messages) - 1; i >= 0; i-- { // oldest to newest
			if Meaningful(thread.Messages[i]) {
				latest = &thread.Messages[i]
				break
			}
		}
		currentKey := ""
		if latest != nil && me[BareAddress(latest.From)] {
			currentKey = db.WaitingKey(*latest)
		}
		for _, msg := range thread.Messages {
			if key := db.WaitingKey(msg); snoozedKeys[key] && key != currentKey {
				stale = append(stale, key)
			}
		}
		if latest == nil || !me[BareAddress(latest.From)] {
			continue
		}
		var waitingFor []Participant
		seen := map[string]bool{}
		for _, p := range ParseParticipants(latest.To, latest.CC) {
			if p.Addr == "" || me[p.Addr] || AutomatedAddress(p.Addr) || seen[p.Addr] {
				continue
			}
			seen[p.Addr] = true
			waitingFor = append(waitingFor, p)
		}
		if len(waitingFor) == 0 {
			continue // self-mail, or only robots and lists
		}
		key := currentKey
		if stoppedKeys[key] {
			stopped[latest.ID] = key
		}
		// Snoozed and stopped both hide the wait; neither clears the other.
		if stoppedKeys[key] || snoozedKeys[key] {
			continue
		}
		waiting = append(waiting, WaitingThread{Message: *latest, Key: key, Since: latest.Date, WaitingFor: waitingFor})
	}
	sort.SliceStable(waiting, func(i, j int) bool {
		if !waiting[i].Since.Equal(waiting[j].Since) {
			return waiting[i].Since.Before(waiting[j].Since)
		}
		return waiting[i].Message.ID < waiting[j].Message.ID
	})
	return waiting, stopped, stale
}
