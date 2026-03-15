package monitor

import (
	"database/sql"
	"log"
	"time"

	"example.com/app/api/internal/ws"
)

var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

type changedDevice struct {
	DeviceID   int64
	DeviceKey  string
	LastSeenAt sql.NullTime
}

// Start は 5 秒ごとに offline 判定を実行し、状態変化をイベントとして記録・配信します。
func Start(db *sql.DB, hub *ws.Hub) {
	if db == nil {
		log.Println("monitor: db is nil, monitor is disabled")
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		runOnce(db, hub)
		<-ticker.C
	}
}

func runOnce(db *sql.DB, hub *ws.Hub) {
	rows, err := db.Query(`
		UPDATE device_state ds
		SET online = FALSE,
		    updated_at = NOW()
		FROM devices d
		WHERE ds.device_id = d.id
		  AND ds.online = TRUE
		  AND ds.last_seen_at < NOW() - INTERVAL '30 seconds'
		RETURNING ds.device_id, d.device_key, ds.last_seen_at
	`)
	if err != nil {
		log.Printf("monitor: update offline error: %v", err)
		return
	}
	defer rows.Close()

	changed := make([]changedDevice, 0)
	for rows.Next() {
		var c changedDevice
		if err := rows.Scan(&c.DeviceID, &c.DeviceKey, &c.LastSeenAt); err != nil {
			log.Printf("monitor: scan changed row error: %v", err)
			continue
		}
		changed = append(changed, c)
	}

	for _, c := range changed {
		now := time.Now().In(jst)
		if _, err := db.Exec(`
			INSERT INTO device_events (device_id, ts, level, message)
			VALUES ($1, $2, 'WARN', 'DEVICE_OFFLINE')
		`, c.DeviceID, now); err != nil {
			log.Printf("monitor: insert event error (device=%s): %v", c.DeviceKey, err)
		}

		var lastSeen *time.Time
		if c.LastSeenAt.Valid {
			t := c.LastSeenAt.Time.In(jst)
			lastSeen = &t
		}

		if hub != nil {
			hub.Broadcast(ws.Event{
				Type: "device_state_changed",
				Data: map[string]any{
					"deviceKey":  c.DeviceKey,
					"online":     false,
					"lastSeenAt": lastSeen,
				},
			})
			hub.Broadcast(ws.Event{
				Type: "device_event",
				Data: map[string]any{
					"deviceKey": c.DeviceKey,
					"level":     "WARN",
					"message":   "DEVICE_OFFLINE",
					"ts":        now,
				},
			})
		}

		log.Printf("monitor: device offline detected (device=%s)", c.DeviceKey)
	}
}
