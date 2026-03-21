// Package api — eventbus.go implements an in-memory pub/sub for SSE streaming.
//
// Components publish events (sandbox lifecycle, health ticks) to the bus.
// SSE clients subscribe and receive events in real-time.
//
//	Publisher (handler/reaper)          EventBus           SSE Client
//	         │                             │                    │
//	         │── Publish(event) ──────────▶│                    │
//	         │                             │── fan out ────────▶│
//	         │                             │── fan out ────────▶│ (N clients)
package api

import (
	"sync"
	"time"
)

// Event is a server-sent event.
type Event struct {
	Type      string `json:"type"`      // e.g. "sandbox.created", "health.tick"
	Data      any    `json:"data"`      // event payload
	Timestamp string `json:"timestamp"` // ISO 8601
}

// EventBus fans out events to all connected SSE clients.
type EventBus struct {
	mu      sync.RWMutex
	clients map[chan *Event]struct{}
}

// NewEventBus creates an event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		clients: make(map[chan *Event]struct{}),
	}
}

// Subscribe returns a channel that receives events. Call Unsubscribe when done.
func (b *EventBus) Subscribe() chan *Event {
	ch := make(chan *Event, 64) // buffered to avoid blocking publishers
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a client channel.
func (b *EventBus) Unsubscribe(ch chan *Event) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish sends an event to all connected clients.
// Non-blocking: if a client's buffer is full, the event is dropped for that client.
func (b *EventBus) Publish(eventType string, data any) {
	event := &Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- event:
		default:
			// Client buffer full — drop event to avoid blocking.
		}
	}
}

// ClientCount returns the number of connected SSE clients.
func (b *EventBus) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}
