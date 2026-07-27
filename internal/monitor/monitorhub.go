package monitor

import (
	"log/slog"
	"sync"
)

const sendBuffer = 32

type client struct {
	sessionID uint
	send      chan []byte
}

type hub struct {
	mu      sync.Mutex
	clients map[uint]map[*client]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[uint]map[*client]struct{})}
}

func (h *hub) add(sessionID uint) *client {
	c := &client{sessionID: sessionID, send: make(chan []byte, sendBuffer)}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[sessionID] == nil {
		h.clients[sessionID] = make(map[*client]struct{})
	}
	h.clients[sessionID][c] = struct{}{}
	return c
}

func (h *hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	clients := h.clients[c.sessionID]
	delete(clients, c)
	if len(clients) == 0 {
		delete(h.clients, c.sessionID)
	}
}

func (h *hub) broadcast(sessionID uint, msg []byte) {
	h.mu.Lock()
	targets := make([]*client, 0, len(h.clients[sessionID]))
	for c := range h.clients[sessionID] {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		select {
		case c.send <- msg:
		default:
			// ponytail: drop when a client is not draining; add per-client backlog if drops show up in logs
			slog.Warn("monitor client is not keeping up, dropping event", "session", sessionID)
		}
	}
}
