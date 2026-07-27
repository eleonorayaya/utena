package monitor

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/eleonorayaya/utena/internal/eventbus"
)

const sendBuffer = 32

type SnapshotProvider interface {
	SessionSnapshot(ctx context.Context, sessionID uint) []eventbus.SessionNotificationEvent
}

type client struct {
	sessionID uint
	send      chan []byte
}

type MonitorService struct {
	eventBus  eventbus.EventBus
	snapshots SnapshotProvider

	mu      sync.Mutex
	clients map[uint]map[*client]struct{}
}

func NewMonitorService(bus eventbus.EventBus, snapshots SnapshotProvider) *MonitorService {
	return &MonitorService{
		eventBus:  bus,
		snapshots: snapshots,
		clients:   make(map[uint]map[*client]struct{}),
	}
}

func (s *MonitorService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.SessionNotification, s.handleSessionNotification)
	return nil
}

func (s *MonitorService) OnAppEnd(ctx context.Context) error {
	return nil
}

// Stream sends the session's current state, then every later event for it, to
// write. It returns when ctx is done or write fails.
func (s *MonitorService) Stream(ctx context.Context, sessionID uint, write func([]byte) error) error {
	c := s.add(sessionID)
	defer s.remove(c)

	for _, event := range s.snapshots.SessionSnapshot(ctx, sessionID) {
		msg, err := json.Marshal(event)
		if err != nil {
			slog.Warn("failed to encode monitor snapshot", "type", event.Type, "error", err)
			continue
		}
		if err := write(msg); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-c.send:
			if err := write(msg); err != nil {
				return err
			}
		}
	}
}

func (s *MonitorService) handleSessionNotification(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionNotificationEvent)
	if !ok {
		return nil
	}

	msg, err := json.Marshal(data)
	if err != nil {
		slog.Warn("failed to encode monitor notification", "type", data.Type, "error", err)
		return nil
	}
	s.broadcast(data.SessionID, msg)
	return nil
}

func (s *MonitorService) add(sessionID uint) *client {
	c := &client{sessionID: sessionID, send: make(chan []byte, sendBuffer)}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clients[sessionID] == nil {
		s.clients[sessionID] = make(map[*client]struct{})
	}
	s.clients[sessionID][c] = struct{}{}
	return c
}

func (s *MonitorService) remove(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clients := s.clients[c.sessionID]
	delete(clients, c)
	if len(clients) == 0 {
		delete(s.clients, c.sessionID)
	}
}

func (s *MonitorService) broadcast(sessionID uint, msg []byte) {
	s.mu.Lock()
	targets := make([]*client, 0, len(s.clients[sessionID]))
	for c := range s.clients[sessionID] {
		targets = append(targets, c)
	}
	s.mu.Unlock()

	for _, c := range targets {
		select {
		case c.send <- msg:
		default:
			// ponytail: drop when a client is not draining, add per-client backlog if drops show up in logs
			slog.Warn("monitor client is not keeping up, dropping event", "session", sessionID)
		}
	}
}
