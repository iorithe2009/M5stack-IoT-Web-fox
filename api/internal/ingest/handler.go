package ingest

import (
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"example.com/app/api/internal/ws"
)

var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

// Payload は MQTT telemetry トピックで受け取る JSON の構造です。
type Payload struct {
	Ts      string             `json:"ts"`
	Metrics map[string]float64 `json:"metrics"`
	Unit    map[string]string  `json:"unit"`
}

type HeartbeatPayload struct {
	Ts string `json:"ts"`
	Fw string `json:"fw"`
	IP string `json:"ip"`
}

type CommandAckPayload struct {
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Ts        string `json:"ts"`
}

// Handle は MQTT メッセージを受け取り、DB に保存します。
// topic 例: "iot/m5-001/telemetry", "iot/m5-001/heartbeat"
func Handle(db *sql.DB, topic string, data []byte, hub *ws.Hub) {
	// トピックから device_key と種別を取り出す（"iot/{device_key}/{kind}"）
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		log.Printf("ingest: unexpected topic format: %s", topic)
		return
	}
	deviceKey := parts[1]
	kind := parts[2]

	switch kind {
	case "telemetry":
		handleTelemetry(db, deviceKey, data, hub)
	case "heartbeat":
		handleHeartbeat(db, deviceKey, data, hub)
	case "cmd_ack":
		handleCommandAck(db, deviceKey, data, hub)
	default:
		log.Printf("ingest: unsupported topic kind=%s topic=%s", kind, topic)
	}
}

func handleTelemetry(db *sql.DB, deviceKey string, data []byte, hub *ws.Hub) {
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("ingest: JSON parse error on telemetry device=%s: %v", deviceKey, err)
		return
	}

	// デバイス計測時刻（省略時はサーバ受信時刻）
	ts := parseTsOrNow(p.Ts)

	deviceID, err := upsertDevice(db, deviceKey)
	if err != nil {
		log.Printf("ingest: devices upsert error: %v", err)
		return
	}

	wasOnline, exists, err := currentOnlineState(db, deviceID)
	if err != nil {
		log.Printf("ingest: device_state read error: %v", err)
		return
	}

	if err := upsertOnlineState(db, deviceID, ""); err != nil {
		log.Printf("ingest: device_state upsert error: %v", err)
		return
	}

	if exists && !wasOnline {
		emitDeviceOnline(db, hub, deviceID, deviceKey)
	}

	// telemetry に各メトリクスを INSERT
	for metric, value := range p.Metrics {
		unit := ""
		if p.Unit != nil {
			unit = p.Unit[metric]
		}
		_, err = db.Exec(`
			INSERT INTO telemetry (device_id, ts, metric, value, unit)
			VALUES ($1, $2, $3, $4, $5)
		`, deviceID, ts, metric, value, unit)
		if err != nil {
			log.Printf("ingest: telemetry insert error (metric=%s): %v", metric, err)
		}
	}

	log.Printf("ingest: saved %d metrics from device=%s", len(p.Metrics), deviceKey)
}

func handleHeartbeat(db *sql.DB, deviceKey string, data []byte, hub *ws.Hub) {
	var p HeartbeatPayload
	if strings.TrimSpace(string(data)) != "" {
		if err := json.Unmarshal(data, &p); err != nil {
			log.Printf("ingest: JSON parse error on heartbeat device=%s: %v", deviceKey, err)
			return
		}
	}

	deviceID, err := upsertDevice(db, deviceKey)
	if err != nil {
		log.Printf("ingest: devices upsert error: %v", err)
		return
	}

	wasOnline, exists, err := currentOnlineState(db, deviceID)
	if err != nil {
		log.Printf("ingest: device_state read error: %v", err)
		return
	}

	if err := upsertOnlineState(db, deviceID, p.Fw); err != nil {
		log.Printf("ingest: heartbeat state upsert error: %v", err)
		return
	}

	if exists && !wasOnline {
		emitDeviceOnline(db, hub, deviceID, deviceKey)
	}

	log.Printf("ingest: heartbeat received from device=%s", deviceKey)
}

func parseTsOrNow(raw string) time.Time {
	ts := time.Now().In(jst)
	if raw == "" {
		return ts
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(jst)
	}
	return ts
}

func upsertDevice(db *sql.DB, deviceKey string) (int64, error) {
	var deviceID int64
	err := db.QueryRow(`
		INSERT INTO devices (device_key)
		VALUES ($1)
		ON CONFLICT (device_key) DO UPDATE SET device_key = EXCLUDED.device_key
		RETURNING id
	`, deviceKey).Scan(&deviceID)
	return deviceID, err
}

func currentOnlineState(db *sql.DB, deviceID int64) (online bool, exists bool, err error) {
	err = db.QueryRow(`SELECT online FROM device_state WHERE device_id = $1`, deviceID).Scan(&online)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return online, true, nil
}

func upsertOnlineState(db *sql.DB, deviceID int64, fw string) error {
	_, err := db.Exec(`
		INSERT INTO device_state (device_id, online, last_seen_at, fw_version, updated_at)
		VALUES ($1, TRUE, NOW(), NULLIF($2, ''), NOW())
		ON CONFLICT (device_id) DO UPDATE
		SET online = TRUE,
		    last_seen_at = NOW(),
		    fw_version = COALESCE(NULLIF(EXCLUDED.fw_version, ''), device_state.fw_version),
		    updated_at = NOW()
	`, deviceID, fw)
	return err
}

func emitDeviceOnline(db *sql.DB, hub *ws.Hub, deviceID int64, deviceKey string) {
	now := time.Now().In(jst)
	if _, err := db.Exec(`
		INSERT INTO device_events (device_id, ts, level, message)
		VALUES ($1, $2, 'INFO', 'DEVICE_ONLINE')
	`, deviceID, now); err != nil {
		log.Printf("ingest: insert online event error (device=%s): %v", deviceKey, err)
	}

	if hub != nil {
		hub.Broadcast(ws.Event{
			Type: "device_state_changed",
			Data: map[string]any{
				"deviceKey":  deviceKey,
				"online":     true,
				"lastSeenAt": now,
			},
		})
		hub.Broadcast(ws.Event{
			Type: "device_event",
			Data: map[string]any{
				"deviceKey": deviceKey,
				"level":     "INFO",
				"message":   "DEVICE_ONLINE",
				"ts":        now,
			},
		})
	}
}

func handleCommandAck(db *sql.DB, deviceKey string, data []byte, hub *ws.Hub) {
	var p CommandAckPayload
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("ingest: JSON parse error on cmd_ack device=%s: %v", deviceKey, err)
		return
	}
	if strings.TrimSpace(p.RequestID) == "" {
		log.Printf("ingest: empty requestId on cmd_ack device=%s", deviceKey)
		return
	}

	nextStatus := ""
	switch p.Status {
	case "ok":
		nextStatus = "ACK"
	case "error":
		nextStatus = "FAIL"
	default:
		log.Printf("ingest: unsupported ack status device=%s requestId=%s status=%s", deviceKey, p.RequestID, p.Status)
		return
	}

	ackAt := parseTsOrNow(p.Ts)
	row := db.QueryRow(`
		UPDATE commands c
		SET status = $1,
		    ack_at = $2,
		    error_message = CASE WHEN $1 = 'FAIL' THEN $3 ELSE '' END,
		    updated_at = NOW()
		FROM devices d
		WHERE c.device_id = d.id
		  AND c.request_id = $4
		  AND d.device_key = $5
		  AND c.status = 'SENT'
		RETURNING c.id, c.command_type, c.payload, c.created_at, c.sent_at, c.ack_at, c.timeout_at
	`, nextStatus, ackAt, p.Message, p.RequestID, deviceKey)

	var commandID int64
	var commandType string
	var payload []byte
	var createdAt time.Time
	var sentAt sql.NullTime
	var ackAtDB sql.NullTime
	var timeoutAt sql.NullTime
	err := row.Scan(&commandID, &commandType, &payload, &createdAt, &sentAt, &ackAtDB, &timeoutAt)
	if err == sql.ErrNoRows {
		log.Printf("ingest: cmd_ack ignored device=%s requestId=%s", deviceKey, p.RequestID)
		return
	}
	if err != nil {
		log.Printf("ingest: cmd_ack update error device=%s requestId=%s: %v", deviceKey, p.RequestID, err)
		return
	}

	if hub != nil {
		eventData := map[string]any{
			"id":           commandID,
			"requestId":    p.RequestID,
			"deviceKey":    deviceKey,
			"commandType":  commandType,
			"payload":      json.RawMessage(payload),
			"status":       nextStatus,
			"errorMessage": "",
			"createdAt":    createdAt,
			"sentAt":       nil,
			"ackAt":        nil,
			"timeoutAt":    nil,
		}
		if nextStatus == "FAIL" {
			eventData["errorMessage"] = p.Message
		}
		if sentAt.Valid {
			eventData["sentAt"] = sentAt.Time
		}
		if ackAtDB.Valid {
			eventData["ackAt"] = ackAtDB.Time
		}
		if timeoutAt.Valid {
			eventData["timeoutAt"] = timeoutAt.Time
		}

		hub.Broadcast(ws.Event{
			Type: "command_updated",
			Data: eventData,
		})
	}

	log.Printf("ingest: cmd_ack updated device=%s requestId=%s status=%s", deviceKey, p.RequestID, nextStatus)
}
