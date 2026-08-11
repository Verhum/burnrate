package server

import (
	"encoding/json"
	"sync"
)

type sseEvent struct {
	Type string
	Data []byte
}

type sseHub struct {
	mu      sync.Mutex
	clients map[chan sseEvent]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{
		clients: make(map[chan sseEvent]struct{}),
	}
}

func (h *sseHub) subscribe() (<-chan sseEvent, func()) {
	ch := make(chan sseEvent, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.clients, ch)
		close(ch)
		h.mu.Unlock()
	}
}

func (h *sseHub) broadcast(eventType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := sseEvent{Type: eventType, Data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}
