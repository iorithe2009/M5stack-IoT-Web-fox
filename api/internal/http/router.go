package httpapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	mqttclient "example.com/app/api/internal/mqtt"
	"example.com/app/api/internal/ws"
)

// ---------- IoT 用の型定義 ----------

type LatestMetric struct {
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
}

type DeviceItem struct {
	ID           int64          `json:"id"`
	DeviceKey    string         `json:"deviceKey"`
	Name         string         `json:"name"`
	Online       bool           `json:"online"`
	LastSeenAt   *time.Time     `json:"lastSeenAt"`
	LatestValues []LatestMetric `json:"latestValues"`
}

type TelemetryPoint struct {
	Ts    time.Time `json:"ts"`
	Value float64   `json:"value"`
	Unit  string    `json:"unit"`
}

type CommandItem struct {
	ID           int64           `json:"id"`
	RequestID    string          `json:"requestId"`
	DeviceKey    string          `json:"deviceKey,omitempty"`
	CommandType  string          `json:"commandType"`
	Payload      json.RawMessage `json:"payload"`
	Status       string          `json:"status"`
	ErrorMessage string          `json:"errorMessage"`
	CreatedAt    time.Time       `json:"createdAt"`
	SentAt       *time.Time      `json:"sentAt"`
	AckAt        *time.Time      `json:"ackAt"`
	TimeoutAt    *time.Time      `json:"timeoutAt"`
}

type createCommandRequest struct {
	CommandType string          `json:"commandType"`
	Payload     json.RawMessage `json:"payload"`
}

type mqttCommandPayload struct {
	RequestID   string          `json:"requestId"`
	CommandType string          `json:"commandType"`
	Payload     json.RawMessage `json:"payload"`
	Ts          time.Time       `json:"ts"`
}

type Server struct {
	DB         *sql.DB
	CORSOrigin string
	Hub        *ws.Hub
	MQTT       *mqttclient.Client
	Mux        *http.ServeMux
}

func New(db *sql.DB, corsOrigin string, hub *ws.Hub, mqtt *mqttclient.Client) *Server {
	s := &Server{DB: db, CORSOrigin: corsOrigin, Hub: hub, MQTT: mqtt, Mux: http.NewServeMux()}

	// routes
	s.Mux.HandleFunc("/api/hello", s.handleHello)
	s.Mux.HandleFunc("/api/messages", s.handleMessages)
	s.Mux.HandleFunc("/api/devices", s.handleDevices)
	s.Mux.HandleFunc("/api/devices/", s.handleDeviceSubresources)
	s.Mux.HandleFunc("/ws", s.handleWS)

	return s
}

func (s *Server) Handler() http.Handler {
	return s.corsMiddleware(s.loggingMiddleware(s.Mux))
}

func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("Hello World!!"))
}

type Message struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.DB == nil {
		http.Error(w, "DB is not configured", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := s.DB.Query(`SELECT id, body, created_at FROM messages ORDER BY id DESC LIMIT 100`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		msgs := make([]Message, 0)
		for rows.Next() {
			var m Message
			if err := rows.Scan(&m.ID, &m.Body, &m.CreatedAt); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			msgs = append(msgs, m)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(msgs)

	case http.MethodPost:
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body := strings.TrimSpace(string(b))
		if body == "" {
			http.Error(w, "empty body", http.StatusBadRequest)
			return
		}
		var id int64
		err = s.DB.QueryRow(`INSERT INTO messages(body) VALUES ($1) RETURNING id`, body).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDevices: GET /api/devices
// デバイス一覧（online 状態・各メトリクスの最新値）を返す
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, "DB is not configured", http.StatusServiceUnavailable)
		return
	}

	rows, err := s.DB.Query(`
		SELECT d.id, d.device_key, d.name,
		       COALESCE(ds.online, FALSE),
		       ds.last_seen_at
		FROM devices d
		LEFT JOIN device_state ds ON ds.device_id = d.id
		ORDER BY d.id
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	devices := make([]DeviceItem, 0)
	for rows.Next() {
		var dev DeviceItem
		var lastSeen sql.NullTime
		if err := rows.Scan(&dev.ID, &dev.DeviceKey, &dev.Name, &dev.Online, &lastSeen); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if lastSeen.Valid {
			dev.LastSeenAt = &lastSeen.Time
		}
		dev.LatestValues = make([]LatestMetric, 0)
		devices = append(devices, dev)
	}
	rows.Close()

	// 各デバイスの最新メトリクスを取得
	for i, dev := range devices {
		mrows, err := s.DB.Query(`
			SELECT DISTINCT ON (metric) metric, value, unit
			FROM telemetry
			WHERE device_id = $1
			ORDER BY metric, ts DESC
		`, dev.ID)
		if err != nil {
			continue
		}
		for mrows.Next() {
			var m LatestMetric
			if err := mrows.Scan(&m.Metric, &m.Value, &m.Unit); err == nil {
				devices[i].LatestValues = append(devices[i].LatestValues, m)
			}
		}
		mrows.Close()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(devices)
}

// handleDeviceTelemetry: GET /api/devices/{device_key}/telemetry?metric=temp&duration=1h
// 指定デバイス・メトリクスの時系列データを返す（デフォルト1h、最大24h）
func (s *Server) handleDeviceSubresources(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	switch parts[1] {
	case "telemetry":
		s.handleDeviceTelemetry(w, r, parts[0])
	case "commands":
		s.handleDeviceCommands(w, r, parts[0])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleDeviceTelemetry(w http.ResponseWriter, r *http.Request, deviceKey string) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.DB == nil {
		http.Error(w, "DB is not configured", http.StatusServiceUnavailable)
		return
	}

	// URL: /api/devices/{device_key}/telemetry
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "temp"
	}

	durationStr := r.URL.Query().Get("duration")
	var duration time.Duration
	switch durationStr {
	case "24h":
		duration = 24 * time.Hour
	default:
		duration = 1 * time.Hour
	}

	since := time.Now().UTC().Add(-duration)

	rows, err := s.DB.Query(`
		SELECT t.ts, t.value, t.unit
		FROM telemetry t
		JOIN devices d ON d.id = t.device_id
		WHERE d.device_key = $1
		  AND t.metric = $2
		  AND t.ts >= $3
		ORDER BY t.ts ASC
	`, deviceKey, metric, since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	points := make([]TelemetryPoint, 0)
	for rows.Next() {
		var p TelemetryPoint
		if err := rows.Scan(&p.Ts, &p.Value, &p.Unit); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		points = append(points, p)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(points)
}

func (s *Server) handleDeviceCommands(w http.ResponseWriter, r *http.Request, deviceKey string) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.DB == nil {
		http.Error(w, "DB is not configured", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetCommands(w, r, deviceKey)
	case http.MethodPost:
		s.handlePostCommand(w, r, deviceKey)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetCommands(w http.ResponseWriter, r *http.Request, deviceKey string) {
	deviceID, err := s.lookupDeviceID(deviceKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}

	rows, err := s.DB.Query(`
		SELECT id, request_id, command_type, payload, status, error_message,
		       created_at, sent_at, ack_at, timeout_at
		FROM commands
		WHERE device_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]CommandItem, 0)
	for rows.Next() {
		item, err := scanCommand(rows, deviceKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(items)
}

func (s *Server) handlePostCommand(w http.ResponseWriter, r *http.Request, deviceKey string) {
	if s.MQTT == nil {
		http.Error(w, "mqtt is not configured", http.StatusServiceUnavailable)
		return
	}

	deviceID, err := s.lookupDeviceID(deviceKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req createCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	payload, err := validateCommandPayload(req.CommandType, req.Payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	requestID, err := newRequestID()
	if err != nil {
		http.Error(w, "failed to generate request id", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	var commandID int64
	if err := s.DB.QueryRow(`
		INSERT INTO commands (
			device_id, request_id, command_type, payload, status,
			requested_by, error_message, sent_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4::jsonb, 'SENT', 'web', '', $5, $5, $5)
		RETURNING id
	`, deviceID, requestID, req.CommandType, payload, now).Scan(&commandID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	message := mqttCommandPayload{
		RequestID:   requestID,
		CommandType: req.CommandType,
		Payload:     payload,
		Ts:          now,
	}
	if err := s.MQTT.PublishCommand(deviceKey, message); err != nil {
		_, _ = s.DB.Exec(`
			UPDATE commands
			SET status = 'FAIL',
			    error_message = $2,
			    updated_at = NOW()
			WHERE id = $1
		`, commandID, err.Error())
		http.Error(w, fmt.Sprintf("mqtt publish failed: %v", err), http.StatusInternalServerError)
		return
	}

	item := CommandItem{
		ID:           commandID,
		RequestID:    requestID,
		DeviceKey:    deviceKey,
		CommandType:  req.CommandType,
		Payload:      payload,
		Status:       "SENT",
		ErrorMessage: "",
		CreatedAt:    now,
		SentAt:       &now,
	}

	s.broadcastCommandUpdate(item)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(item)
}

func (s *Server) lookupDeviceID(deviceKey string) (int64, error) {
	var deviceID int64
	err := s.DB.QueryRow(`SELECT id FROM devices WHERE device_key = $1`, deviceKey).Scan(&deviceID)
	return deviceID, err
}

func validateCommandPayload(commandType string, payload json.RawMessage) (json.RawMessage, error) {
	switch commandType {
	case "LED_SET":
		var p struct {
			LEDOn *bool `json:"ledOn"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("invalid payload")
		}
		if p.LEDOn == nil {
			return nil, fmt.Errorf("payload.ledOn is required")
		}
	case "SAMPLING_INTERVAL_SET":
		var p struct {
			Seconds *int `json:"seconds"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("invalid payload")
		}
		if p.Seconds == nil || *p.Seconds < 1 || *p.Seconds > 3600 {
			return nil, fmt.Errorf("payload.seconds must be between 1 and 3600")
		}
	default:
		return nil, fmt.Errorf("unsupported commandType")
	}

	if len(payload) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return payload, nil
}

func newRequestID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("cmd_%x", b[:]), nil
}

func scanCommand(scanner interface {
	Scan(dest ...any) error
}, deviceKey string) (CommandItem, error) {
	var item CommandItem
	var payload []byte
	var sentAt sql.NullTime
	var ackAt sql.NullTime
	var timeoutAt sql.NullTime
	err := scanner.Scan(
		&item.ID,
		&item.RequestID,
		&item.CommandType,
		&payload,
		&item.Status,
		&item.ErrorMessage,
		&item.CreatedAt,
		&sentAt,
		&ackAt,
		&timeoutAt,
	)
	if err != nil {
		return item, err
	}
	item.DeviceKey = deviceKey
	item.Payload = json.RawMessage(payload)
	if sentAt.Valid {
		item.SentAt = &sentAt.Time
	}
	if ackAt.Valid {
		item.AckAt = &ackAt.Time
	}
	if timeoutAt.Valid {
		item.TimeoutAt = &timeoutAt.Time
	}
	return item, nil
}

func (s *Server) broadcastCommandUpdate(item CommandItem) {
	if s.Hub == nil {
		return
	}

	s.Hub.Broadcast(ws.Event{
		Type: "command_updated",
		Data: item,
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := s.CORSOrigin
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Hub == nil {
		http.Error(w, "websocket hub is not configured", http.StatusServiceUnavailable)
		return
	}
	s.Hub.HandleWS(w, r)
}
