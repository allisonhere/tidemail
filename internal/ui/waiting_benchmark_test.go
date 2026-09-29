package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

func benchmarkWaitingMessages(n int) []db.Message {
	messages := make([]db.Message, n)
	for i := range messages {
		messages[i] = db.Message{
			ID:        int64(i + 1),
			MessageID: fmt.Sprintf("<message-%d@example.com>", i),
			From:      "me@example.com",
			To:        "person@example.com",
			Subject:   "Subject",
			Date:      time.Unix(int64(i+1), 0),
		}
	}
	return messages
}

func BenchmarkComputeWaiting(b *testing.B) {
	me := map[string]bool{"me@example.com": true}
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("messages-%d", n), func(b *testing.B) {
			messages := benchmarkWaitingMessages(n)
			b.ReportMetric(float64(n), "messages")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				computeWaiting(messages, me, nil)
			}
		})
	}
}
