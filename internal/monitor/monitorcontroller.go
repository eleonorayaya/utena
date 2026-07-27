package monitor

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/eleonorayaya/utena/internal/common"
)

type MonitorController struct {
	service *MonitorService
}

func NewMonitorController(service *MonitorService) *MonitorController {
	return &MonitorController{service: service}
}

func (c *MonitorController) Watch(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("session_id")
	sessionID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || sessionID == 0 {
		slog.Warn("monitor connection rejected", "session_id", raw)
		common.RenderError(w, r, common.NewInvalidRequest("session_id query param is required"))
		return
	}

	client := c.service.attach(uint(sessionID))
	defer c.service.detach(client)

	sock, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Warn("failed to accept monitor websocket", "session", sessionID, "error", err)
		return
	}
	defer func() { _ = sock.CloseNow() }()

	ctx := sock.CloseRead(r.Context())
	slog.Info("monitor client connected", "session", sessionID)

	for _, msg := range c.service.snapshot(ctx, uint(sessionID)) {
		if err := sock.Write(ctx, websocket.MessageText, msg); err != nil {
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("monitor client disconnected", "session", sessionID)
			return
		case msg := <-client.send:
			if err := sock.Write(ctx, websocket.MessageText, msg); err != nil {
				slog.Info("monitor client write failed", "session", sessionID, "error", err)
				return
			}
		}
	}
}
