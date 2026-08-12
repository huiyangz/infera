package realtime

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestBroadcastReachesSubscribedClient(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	did := uuid.New()
	ch := h.Subscribe(did)
	defer h.Unsubscribe(did, ch)

	h.Broadcast(did, Event{Type: "stage_started", Stage: "spec"})

	select {
	case e := <-ch:
		assert.Equal(t, "stage_started", e.Type)
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestBroadcastDoesNotReachOtherDelivery(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	a := uuid.New()
	b := uuid.New()
	chA := h.Subscribe(a)
	defer h.Unsubscribe(a, chA)
	_ = h.Subscribe(b)

	h.Broadcast(b, Event{Type: "x"})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); time.Sleep(100 * time.Millisecond) }()
	wg.Wait()

	select {
	case e := <-chA:
		t.Fatalf("delivery A should not get B's event, got %+v", e)
	default:
		// good
	}
}
