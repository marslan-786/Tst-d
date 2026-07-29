package main

import (
	"log"
	"net/http"
	"time"
  "encoding/json"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (same-origin in production via Railway)
	},
}

// WebSocketHub manages all connected WebSocket clients.
type WebSocketHub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

// NewWebSocketHub creates a new hub.
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop.
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("[hub] Client connected. Total: %d", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			log.Printf("[hub] Client disconnected. Total: %d", len(h.clients))

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Register adds a client to the hub.
func (h *WebSocketHub) Register(c *Client) {
	h.register <- c
}

// Unregister removes a client from the hub.
func (h *WebSocketHub) Unregister(c *Client) {
	h.unregister <- c
}

// Broadcast sends a message to all connected clients.
func (h *WebSocketHub) Broadcast(msg []byte) {
	h.broadcast <- msg
}

// BroadcastLog is a helper to send a log entry as JSON.
func (h *WebSocketHub) BroadcastLog(level, message string) {
	entry := LogEntry{Type: "log", Level: level, Message: message}
	data, _ := json.Marshal(entry)
	h.Broadcast(data)
}

// Client represents a single WebSocket connection.
type Client struct {
	hub       *WebSocketHub
	conn      *websocket.Conn
	send      chan []byte
	orch      *Orchestrator
}

// NewClient creates a new WebSocket client.
func NewClient(hub *WebSocketHub, conn *websocket.Conn, orch *Orchestrator) *Client {
	return &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256),
		orch: orch,
	}
}

// ReadPump reads messages from the WebSocket connection.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(4096)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// WritePump writes messages to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			// Send stats snapshot every 500ms
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			snapshot := c.orch.StatsSnapshot()
			data, _ := json.Marshal(snapshot)
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}
