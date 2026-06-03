package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// --- WebSocket Protocol Models ---

// WSClientMessage represents a message sent by the client over WebSocket.
type WSClientMessage struct {
	Action        string       `json:"action"`
	AccessToken   string       `json:"accessToken,omitempty"`
	LastMessageID string       `json:"lastMessageId,omitempty"`
	ClientInfo    *ClientInfo  `json:"clientInfo,omitempty"`
	Timestamp     int64        `json:"timestamp,omitempty"`
	Preferences   *Preferences `json:"preferences,omitempty"`
}

// WSServerMessage represents a message sent by the server over WebSocket.
type WSServerMessage struct {
	Action        string          `json:"action"`
	MessageID     string          `json:"messageId,omitempty"`
	NotifyChanged *WSNotification `json:"notifyChanged,omitempty"`
	Errors        []APIError      `json:"errors,omitempty"`
	Meta          Meta            `json:"meta"`
}

// WSNotification represents the notification payload.
type WSNotification struct {
	Type       string      `json:"type"`
	ClientInfo *ClientInfo `json:"clientInfo,omitempty"`
	Message    string      `json:"message"`
}

// --- WebSocket Hub ---

// wsClient represents a single WebSocket connection.
type wsClient struct {
	hub        *wsHub
	conn       *websocket.Conn
	send       chan []byte
	clientInfo *ClientInfo
	authed     bool
	subject    string
}

// wsHub manages all active WebSocket clients and broadcasts messages.
type wsHub struct {
	server     *Server
	clients    map[*wsClient]struct{}
	register   chan *wsClient
	unregister chan *wsClient
	broadcast  chan []byte
	mu         sync.RWMutex
	msgCounter atomic.Int64
}

func newWSHub(s *Server) *wsHub {
	return &wsHub{
		server:     s,
		clients:    make(map[*wsClient]struct{}),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		broadcast:  make(chan []byte, 256),
	}
}

func (h *wsHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					go func(c *wsClient) {
						h.unregister <- c
					}(client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// nextMessageID generates a unique message ID.
func (h *wsHub) nextMessageID() string {
	return fmt.Sprintf("msg-%d", h.msgCounter.Add(1))
}

// Broadcast sends a notification to all connected clients.
func (h *wsHub) Broadcast(notification *WSNotification) {
	msg := WSServerMessage{
		Action:        "notify",
		MessageID:     h.nextMessageID(),
		NotifyChanged: notification,
		Meta:          h.server.meta(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ws: failed to marshal broadcast: %v\n", err)
		return
	}
	h.broadcast <- data
}

// --- WebSocket Upgrader ---

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins; tighten in production via reverse proxy
	},
}

// --- WebSocket Constants ---

const (
	wsWriteWait      = 10 * time.Second
	wsPongWait       = 60 * time.Second
	wsPingPeriod     = (wsPongWait * 9) / 10
	wsMaxMessageSize = 4096
)

// --- WebSocket Handler ---

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.wsHub == nil {
		http.Error(w, "WebSocket not enabled", http.StatusNotFound)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ws: upgrade failed: %v\n", err)
		return
	}

	client := &wsClient{
		hub:  s.wsHub,
		conn: conn,
		send: make(chan []byte, 256),
	}
	s.wsHub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *wsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(wsMaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.handleMessage(message)
	}
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *wsClient) handleMessage(raw []byte) {
	var msg WSClientMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		c.sendError("InvalidRequest", "invalid JSON message")
		return
	}

	// Update client info if provided
	if msg.ClientInfo != nil {
		c.clientInfo = msg.ClientInfo
	}

	// Authenticate if token provided
	if msg.AccessToken != "" {
		if c.hub.server.authService != nil && c.hub.server.authService.Enabled() {
			if c.hub.server.authService.ValidateAccess(msg.AccessToken) {
				parsed, err := c.hub.server.authService.ParseAccess(msg.AccessToken)
				if err == nil {
					c.authed = true
					c.subject = parsed.Subject
				}
			}
		}
	}

	switch msg.Action {
	case "ping":
		c.sendPong()
	case "pong":
		// Client responded to our ping; reset deadline
		c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	case "query":
		c.handleQuery(msg)
	default:
		c.sendError("InvalidRequest", fmt.Sprintf("unknown action: %s", msg.Action))
	}
}

func (c *wsClient) sendPong() {
	resp := WSServerMessage{
		Action: "pong",
		Meta:   c.hub.server.meta(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

func (c *wsClient) handleQuery(msg WSClientMessage) {
	// Query returns the latest status: maintenance or update info
	s := c.hub.server

	// Check maintenance status
	maintenanceCfg := s.currentMaintenanceConfig()
	if maintenanceCfg != nil && maintenanceCfg.MaintenanceActive {
		notification := &WSNotification{
			Type:    "maintenance",
			Message: maintenanceCfg.MaintenanceInfo.Message,
		}
		resp := WSServerMessage{
			Action:        "notify",
			MessageID:     c.hub.nextMessageID(),
			NotifyChanged: notification,
			Meta:          s.meta(),
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return
		}
		select {
		case c.send <- data:
		default:
		}
		return
	}

	// No active notification — send a pong-like acknowledgement
	resp := WSServerMessage{
		Action: "pong",
		Meta:   s.meta(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

func (c *wsClient) sendError(errorType, message string) {
	resp := WSServerMessage{
		Action: "notify",
		Errors: []APIError{{
			Error:        "ForClientError",
			ErrorType:    errorType,
			ErrorMessage: message,
		}},
		Meta: c.hub.server.meta(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

// --- Admin broadcast endpoint ---

// AdminBroadcastPayload is the request body for broadcasting a WebSocket notification.
type AdminBroadcastPayload struct {
	Type       string      `json:"type"`
	Message    string      `json:"message"`
	ClientInfo *ClientInfo `json:"clientInfo,omitempty"`
}

func (s *Server) handleAdminBroadcast(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "authentication required")
		return
	}
	if claims.Role != "admin" {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "admin access required")
		return
	}

	if s.wsHub == nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "WebSocket is not enabled")
		return
	}

	var payload AdminBroadcastPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}

	if payload.Type == "" || payload.Message == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "type and message are required")
		return
	}

	notification := &WSNotification{
		Type:       payload.Type,
		Message:    payload.Message,
		ClientInfo: payload.ClientInfo,
	}
	s.wsHub.Broadcast(notification)

	s.writeJSON(w, http.StatusOK, AdminMessageResponse{
		Message: "notification broadcast sent",
		Meta:    s.meta(),
	})
}

// ClientCount returns the number of currently connected WebSocket clients.
func (h *wsHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
