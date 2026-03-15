package monitor

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"example.com/app/api/internal/ws"
)

type commandTimeoutRow struct {
	ID           int64
	RequestID    string
	DeviceKey    string
	CommandType  string
	Payload      []byte
	CreatedAt    time.Time
	SentAt       sql.NullTime
	TimeoutAt    sql.NullTime
	ErrorMessage string
}

func StartCommandTimeout(db *sql.DB, hub *ws.Hub) {
	if db == nil {
		log.Println("monitor: db is nil, command timeout monitor is disabled")
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		runCommandTimeoutOnce(db, hub)
		<-ticker.C
	}
}

func runCommandTimeoutOnce(db *sql.DB, hub *ws.Hub) {
	rows, err := db.Query(`
		UPDATE commands c
		SET status = 'TIMEOUT',
		    timeout_at = NOW(),
		    updated_at = NOW(),
		    error_message = 'ack timeout'
		FROM devices d
		WHERE c.device_id = d.id
		  AND c.status = 'SENT'
		  AND c.sent_at < NOW() - INTERVAL '30 seconds'
		RETURNING c.id, c.request_id, d.device_key, c.command_type, c.payload,
		          c.created_at, c.sent_at, c.timeout_at, c.error_message
	`)
	if err != nil {
		log.Printf("monitor: command timeout update error: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var row commandTimeoutRow
		if err := rows.Scan(
			&row.ID,
			&row.RequestID,
			&row.DeviceKey,
			&row.CommandType,
			&row.Payload,
			&row.CreatedAt,
			&row.SentAt,
			&row.TimeoutAt,
			&row.ErrorMessage,
		); err != nil {
			log.Printf("monitor: command timeout scan error: %v", err)
			continue
		}

		if hub != nil {
			data := map[string]any{
				"id":           row.ID,
				"requestId":    row.RequestID,
				"deviceKey":    row.DeviceKey,
				"commandType":  row.CommandType,
				"payload":      json.RawMessage(row.Payload),
				"status":       "TIMEOUT",
				"errorMessage": row.ErrorMessage,
				"createdAt":    row.CreatedAt,
				"sentAt":       nil,
				"ackAt":        nil,
				"timeoutAt":    nil,
			}
			if row.SentAt.Valid {
				data["sentAt"] = row.SentAt.Time
			}
			if row.TimeoutAt.Valid {
				data["timeoutAt"] = row.TimeoutAt.Time
			}
			hub.Broadcast(ws.Event{
				Type: "command_updated",
				Data: data,
			})
		}

		log.Printf("monitor: command timeout device=%s requestId=%s", row.DeviceKey, row.RequestID)
	}
}
