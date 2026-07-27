package monitor

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/eleonorayaya/utena/internal/eventbus"
)

type SnapshotProvider interface {
	SessionSnapshot(ctx context.Context, sessionID uint) []eventbus.SessionNotificationEvent
}

type MonitorService struct {
	hub       *hub
	eventBus  eventbus.EventBus
	snapshots SnapshotProvider
}

func NewMonitorService(bus eventbus.EventBus, snapshots SnapshotProvider) *MonitorService {
	return &MonitorService{
		hub:       newHub(),
		eventBus:  bus,
		snapshots: snapshots,
	}
}

func (s *MonitorService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.SessionNotification, s.handleSessionNotification)
	return nil
}

func (s *MonitorService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *MonitorService) handleSessionNotification(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionNotificationEvent)
	if !ok {
		return nil
	}

	msg, err := encodeNotification(data)
	if err != nil {
		slog.Warn("failed to encode monitor notification", "type", data.Type, "error", err)
		return nil
	}
	s.hub.broadcast(data.SessionID, msg)
	return nil
}

func (s *MonitorService) attach(sessionID uint) *client {
	return s.hub.add(sessionID)
}

func (s *MonitorService) detach(c *client) {
	s.hub.remove(c)
}

func (s *MonitorService) snapshot(ctx context.Context, sessionID uint) [][]byte {
	if s.snapshots == nil {
		return nil
	}

	var msgs [][]byte
	for _, n := range s.snapshots.SessionSnapshot(ctx, sessionID) {
		msg, err := encodeNotification(n)
		if err != nil {
			slog.Warn("failed to encode monitor snapshot", "type", n.Type, "error", err)
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

type notification struct {
	Type      string `json:"type"`
	SessionID uint   `json:"session_id"`
	Data      any    `json:"data,omitempty"`
}

func encodeNotification(n eventbus.SessionNotificationEvent) ([]byte, error) {
	return json.Marshal(notification{Type: n.Type, SessionID: n.SessionID, Data: n.Data})
}
