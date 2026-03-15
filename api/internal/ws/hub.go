package ws

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Event はクライアントへ送る WebSocket メッセージの共通形式。
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type Hub struct {
	mu       sync.RWMutex
	clients  map[*client]struct{}
	upgrader websocket.Upgrader
}

func NewHub(corsOrigin string) *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return originAllowed(corsOrigin, r)
			},
		},
	}
}

func originAllowed(corsOrigin string, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	allowed, err := url.Parse(corsOrigin)
	if err != nil || allowed.Scheme == "" || allowed.Host == "" {
		return false
	}

	got, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return strings.EqualFold(allowed.Scheme, got.Scheme) && strings.EqualFold(allowed.Host, got.Host)
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade error: %v", err)
		return
	}

	c := &client{conn: conn}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	count := len(h.clients)
	h.mu.Unlock()
	log.Printf("ws: client connected (total=%d)", count)

	go h.readLoop(c)
}

func (h *Hub) readLoop(c *client) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		count := len(h.clients)
		h.mu.Unlock()
		_ = c.conn.Close()
		log.Printf("ws: client disconnected (total=%d)", count)
	}()

	_ = c.conn.SetReadDeadline(time.Time{})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) Broadcast(ev Event) {
	h.mu.RLock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		c.mu.Lock()
		_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := c.conn.WriteJSON(ev)
		_ = c.conn.SetWriteDeadline(time.Time{})
		c.mu.Unlock()
		if err != nil {
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			_ = c.conn.Close()
			log.Printf("ws: write error, client removed: %v", err)
		}
	}
}
