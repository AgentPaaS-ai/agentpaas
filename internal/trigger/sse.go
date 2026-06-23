package trigger

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const sseContentType = "text/event-stream"

var heartbeatInterval = 15 * time.Second

// SSEHandler handles Server-Sent Events for a specific run.
type SSEHandler struct {
	bus *EventBus
}

// NewSSEHandler creates an SSE handler backed by the given event bus.
func NewSSEHandler(bus *EventBus) *SSEHandler {
	return &SSEHandler{bus: bus}
}

// ServeSSE handles an SSE request for a specific run.
func (h *SSEHandler) ServeSSE(w http.ResponseWriter, r *http.Request, runID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	fromEventID := int64(0)
	if lastIDStr := r.Header.Get("Last-Event-ID"); lastIDStr != "" {
		if id, err := strconv.ParseInt(lastIDStr, 10, 64); err == nil {
			fromEventID = id
		}
	}

	w.Header().Set("Content-Type", sseContentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := h.bus.Subscribe(runID, fromEventID)
	defer cancel()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			if err := writeSSEEvent(w, flusher, &event); err != nil {
				return
			}
			if event.IsTerminal() {
				return
			}
		case <-ticker.C:
			if err := writeSSEHeartbeat(w, flusher); err != nil {
				return
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event *RunEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.EventID, event.Type, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeSSEHeartbeat(w http.ResponseWriter, flusher http.Flusher) error {
	if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
