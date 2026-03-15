package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateCommandPayload(t *testing.T) {
	t.Parallel()

	ledPayload := json.RawMessage(`{"ledOn":true}`)
	got, err := validateCommandPayload("LED_SET", ledPayload)
	if err != nil {
		t.Fatalf("validateCommandPayload returned error: %v", err)
	}
	if string(got) != string(ledPayload) {
		t.Fatalf("expected payload %s, got %s", ledPayload, got)
	}

	_, err = validateCommandPayload("LED_SET", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "payload.ledOn is required") {
		t.Fatalf("expected ledOn validation error, got %v", err)
	}

	_, err = validateCommandPayload("SAMPLING_INTERVAL_SET", json.RawMessage(`{"seconds":0}`))
	if err == nil || !strings.Contains(err.Error(), "payload.seconds must be between 1 and 3600") {
		t.Fatalf("expected seconds range error, got %v", err)
	}

	_, err = validateCommandPayload("UNKNOWN", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported commandType") {
		t.Fatalf("expected unsupported commandType error, got %v", err)
	}
}

func TestNewRequestID(t *testing.T) {
	t.Parallel()

	id, err := newRequestID()
	if err != nil {
		t.Fatalf("newRequestID returned error: %v", err)
	}
	if !strings.HasPrefix(id, "cmd_") {
		t.Fatalf("expected request id to start with cmd_, got %s", id)
	}
	if len(id) != 28 {
		t.Fatalf("expected request id length 28, got %d (%s)", len(id), id)
	}
}
