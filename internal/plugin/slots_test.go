package plugin

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// Many simultaneous message calls, as a large manual bulk run or a burst of
// events would make, never exceed one process per plugin or the global cap.
func TestMessageCallsRespectConcurrencyLimits(t *testing.T) {
	root := t.TempDir()
	ids := []string{"a", "b", "c", "d", "e"}
	for _, id := range ids {
		installPlugin(t, root, id, id, "tidemail-plugin-concurrent", metadataPermission)
	}
	m, err := Discover(root)
	if err != nil || len(m.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, m.Errors())
	}

	var mu sync.Mutex
	maxGlobal, maxOwn := 0, 0
	var wg sync.WaitGroup
	for _, id := range ids {
		for n := range 3 {
			wg.Add(1)
			go func(id string, n int) {
				defer wg.Done()
				res, err := m.MessageMetadata(context.Background(), id, MessageMetadata{ID: int64(n + 1)}, nil)
				if err != nil {
					t.Error(err)
					return
				}
				var got struct{ Global, Own int }
				if err := json.Unmarshal(res.Response.Data, &got); err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				maxGlobal = max(maxGlobal, got.Global)
				maxOwn = max(maxOwn, got.Own)
				mu.Unlock()
			}(id, n)
		}
	}
	wg.Wait()
	if maxOwn != 1 {
		t.Fatalf("a plugin ran %d calls at once", maxOwn)
	}
	if maxGlobal > EventGlobalLimit || maxGlobal < 2 {
		t.Fatalf("max concurrent processes = %d, want 2..%d", maxGlobal, EventGlobalLimit)
	}
	t.Logf("max concurrent: %d", maxGlobal)
}

func TestAcquireRespectsCancellation(t *testing.T) {
	m := &Manager{}
	release, err := m.acquire(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.acquire(ctx, "p"); err == nil {
		t.Fatal("a cancelled wait for a busy plugin should fail")
	}
	release()
	if r, err := m.acquire(context.Background(), "p"); err != nil {
		t.Fatal(err)
	} else {
		r()
	}
}
