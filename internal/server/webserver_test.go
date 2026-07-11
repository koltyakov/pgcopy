package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/koltyakov/pgcopy/internal/state"
)

const (
	expectedSnapshotMessage = "state_snapshot"
	expectedEventMessage    = "state_event"
	expectedProgressMessage = "progress_delta"
)

func TestHandleAPIStateContract(t *testing.T) {
	copyState := state.NewCopyState("test-operation", state.OperationConfig{Parallel: 1})
	ws := NewWebServer(copyState, 8080)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/state", nil)

	ws.handleAPIState(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", contentType)
	}

	message := decodeMessage(t, recorder.Body.Bytes())
	assertMessageKeys(t, message, "type", "state")
	assertMessageType(t, message, expectedSnapshotMessage)

	var snapshot state.CopyStateSnapshot
	if err := json.Unmarshal(message["state"], &snapshot); err != nil {
		t.Fatalf("decode state snapshot: %v", err)
	}
	if snapshot.ID != "test-operation" {
		t.Errorf("snapshot ID = %q, want test-operation", snapshot.ID)
	}
}

func TestWebSocketProtocolContract(t *testing.T) {
	copyState := state.NewCopyState("test-operation", state.OperationConfig{Parallel: 1})
	copyState.AddTable("public", "users", 10)
	copyState.UpdateTableStatus("public", "users", state.TableStatusCopying)
	ws := NewWebServer(copyState, 8080)
	server := httptest.NewServer(http.HandlerFunc(ws.handleWebSocket))
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	initial := readWebSocketMessage(t, conn)
	assertMessageKeys(t, initial, "type", "state", "timestamp")
	assertMessageType(t, initial, expectedSnapshotMessage)

	ws.broadcastStateUpdate(state.Event{Type: state.EventTableStarted})
	event := readWebSocketMessage(t, conn)
	assertMessageKeys(t, event, "type", "event", "timestamp")
	assertMessageType(t, event, expectedEventMessage)

	ws.broadcastSnapshot()
	progress := readWebSocketMessage(t, conn)
	assertMessageKeys(t, progress, "type", "summary", "tables", "timestamp")
	assertMessageType(t, progress, expectedProgressMessage)

	if err := conn.WriteJSON(map[string]any{"type": "completion_ack"}); err != nil {
		t.Fatalf("send completion acknowledgment: %v", err)
	}
	if !ws.WaitForCompletionAck(time.Second) {
		t.Fatal("completion acknowledgment was not received")
	}
}

func readWebSocketMessage(t *testing.T, conn *websocket.Conn) map[string]json.RawMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read WebSocket message: %v", err)
	}
	return decodeMessage(t, payload)
}

func decodeMessage(t *testing.T, payload []byte) map[string]json.RawMessage {
	t.Helper()
	var message map[string]json.RawMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	return message
}

func assertMessageType(t *testing.T, message map[string]json.RawMessage, expected string) {
	t.Helper()
	var messageType string
	if err := json.Unmarshal(message["type"], &messageType); err != nil {
		t.Fatalf("decode message type: %v", err)
	}
	if messageType != expected {
		t.Errorf("message type = %q, want %q", messageType, expected)
	}
}

func assertMessageKeys(t *testing.T, message map[string]json.RawMessage, expected ...string) {
	t.Helper()
	if len(message) != len(expected) {
		t.Fatalf("message keys = %v, want %v", message, expected)
	}
	for _, key := range expected {
		if _, ok := message[key]; !ok {
			t.Errorf("message missing key %q", key)
		}
	}
}
