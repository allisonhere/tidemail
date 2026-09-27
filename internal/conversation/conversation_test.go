package conversation

import (
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

func at(h int) time.Time { return time.Date(2026, 9, 1, h, 0, 0, 0, time.UTC) }

func TestBuildThreadsJoinsReferencesAndKeepsSameSubjectApart(t *testing.T) {
	msgs := []db.Message{
		{ID: 1, MessageID: "<a@x>", Subject: "Hello", Date: at(1)},
		{ID: 2, MessageID: "<b@x>", InReplyTo: "<A@x>", Subject: "Re: Hello", Date: at(2)},
		{ID: 3, MessageID: "<c@x>", References: "<a@x> <b@x>", Subject: "Re: Hello", Date: at(3), Read: true},
		{ID: 4, MessageID: "<d@x>", Subject: "Hello", Date: at(4)}, // unrelated, same subject
	}
	threads := BuildThreads(msgs)
	if len(threads) != 2 {
		t.Fatalf("threads = %d, want 2", len(threads))
	}
	if threads[0].Representative.ID != 4 || threads[1].Count != 3 || threads[1].Representative.ID != 3 || threads[1].UnreadCount != 2 {
		t.Fatalf("unexpected threads: %+v", threads)
	}
}

func TestComputeWaiting(t *testing.T) {
	me := MyAddresses("me@x.example", "Me <ME@x.example>")
	msgs := []db.Message{
		{ID: 1, MessageID: "<1@x>", From: "Ann <ann@y.example>", To: "me@x.example", Date: at(1)},
		{ID: 2, MessageID: "<2@x>", InReplyTo: "<1@x>", From: "me@x.example", To: "Ann <ann@y.example>, noreply@y.example", Date: at(2)},
		{ID: 3, MessageID: "<3@x>", From: "me@x.example", To: "me@x.example", Date: at(3)}, // self-mail
	}
	waiting, _, _ := ComputeWaiting(msgs, me, nil, nil)
	if len(waiting) != 1 || waiting[0].Message.ID != 2 || len(waiting[0].WaitingFor) != 1 || waiting[0].WaitingFor[0].Addr != "ann@y.example" {
		t.Fatalf("waiting = %+v", waiting)
	}
}
