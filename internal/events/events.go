// Package events is a tiny in-process pub/sub used to push live updates to
// browsers over Server-Sent Events.
package events

import (
	"encoding/json"
	"sync"
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 32)
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

// Publish sends to every subscriber, dropping the event for slow clients.
func (h *Hub) Publish(typ string, data any) {
	b, err := json.Marshal(Event{Type: typ, Data: data})
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
