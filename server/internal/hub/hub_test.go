package hub

import "testing"

func TestSlowSubscriberIsClosed(t *testing.T) {
	h := New()
	slow, _ := h.Subscribe()
	fast, cancelFast := h.Subscribe()
	defer cancelFast()
	for i := 0; i < 65; i++ { // one more than the buffer
		h.Publish("x", i)
		<-fast
	}
	n := 0
	for range slow { // drains the 64 buffered events, then sees the close
		n++
	}
	if n != 64 {
		t.Fatalf("got %d events before close, want 64", n)
	}
	h.Publish("x", 1)
	if _, ok := <-fast; !ok {
		t.Fatal("a subscriber that keeps up stays open")
	}
}

func TestCancelAfterCloseIsSafe(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe()
	for i := 0; i < 65; i++ {
		h.Publish("x", i)
	}
	for range ch {
	}
	cancel() // must not close twice
}
