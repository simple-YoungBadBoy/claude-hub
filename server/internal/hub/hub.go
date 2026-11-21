// Package hub fans out JSON events to every connected client.
package hub

import (
	"encoding/json"
	"sync"
)

type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func New() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

// Subscribe returns a channel of encoded events and a cancel func.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish sends {"type": typ, "data": data} to every subscriber; slow
// subscribers drop messages rather than block.
func (h *Hub) Publish(typ string, data any) {
	b, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}
