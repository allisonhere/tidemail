package termimage

import (
	"math/rand/v2"
	"sync"
)

// IDAllocator hands out image ids that are unique for this session while
// staying within the encodable range. The high byte is randomized per session
// so TideMail is unlikely to collide with other graphics-protocol clients in the
// same terminal; the low byte cycles through 1..255 and the high byte advances
// every 255 allocations, giving roughly 75k distinct ids before wrap-around.
type IDAllocator struct {
	mu     sync.Mutex
	next   uint64
	hiBase int
}

// NewIDAllocator returns an allocator with a randomized high byte.
func NewIDAllocator() *IDAllocator {
	return &IDAllocator{hiBase: rand.IntN(MaxGridValue + 1)}
}

// Next returns the next id.
func (a *IDAllocator) Next() uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	low := int(a.next%255) + 1
	hi := (a.hiBase + int((a.next/255)%uint64(MaxGridValue+1))) % (MaxGridValue + 1)
	a.next++
	return MakeID(low, hi)
}
