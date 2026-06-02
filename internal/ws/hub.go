package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message types sent to clients
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// URLResult represents a single URL prewarm result
type URLResult struct {
	MediaSlug  string `json:"mediaSlug"`
	FileSlug   string `json:"fileSlug"`
	Resolution string `json:"resolution"`
	URL        string `json:"url"`
	Status     int    `json:"status"`
	Cache      string `json:"cache"`
	Pop        string `json:"pop"`
	Duration   string `json:"duration"`
	Error      string `json:"error,omitempty"`
	Progress   int64  `json:"progress"`
	Total      int64  `json:"total"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// client wraps a WebSocket connection with a buffered send channel
type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub manages WebSocket connections and broadcasts
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]bool
}

var instance *Hub

func GetHub() *Hub {
	if instance == nil {
		instance = &Hub{
			clients: make(map[*client]bool),
		}
	}
	return instance
}

// HandleWS upgrades HTTP to WebSocket
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("⚠️ WebSocket upgrade failed: %v", err)
		return
	}

	c := &client{
		conn: conn,
		send: make(chan []byte, 256),
	}

	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()

	log.Printf("🔌 WebSocket client connected (%d total)", len(h.clients))

	// Writer goroutine — only one goroutine writes to conn
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			conn.Close()
		}()

		for msg := range c.send {
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Reader goroutine — detect close
	go func() {
		defer func() {
			close(c.send)
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msgType string, data interface{}) {
	msg := Message{Type: msgType, Data: data}
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		select {
		case c.send <- jsonData:
		default:
			// Drop message if buffer full
		}
	}
}
