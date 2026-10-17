// internal/livesplit/server.go
package livesplit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jdharms/sni-autosplitter/internal/engine"
	"github.com/sirupsen/logrus"
)

// Server manages the LiveSplit One WebSocket server
type Server struct {
	logger   *logrus.Logger
	upgrader websocket.Upgrader
	clients  map[*Client]bool
	engine   *engine.SplittingEngine
	mu       sync.RWMutex

	// Server configuration
	host    string
	port    int
	tlsCert string
	tlsKey  string

	// State
	running bool
	server  *http.Server
}

// Client represents a connected LiveSplit One client
type Client struct {
	conn   *websocket.Conn
	server *Server
	send   chan []byte
	logger *logrus.Entry
}

// NewServer creates a new LiveSplit One WebSocket server
func NewServer(logger *logrus.Logger, host string, port int, tlsCert, tlsKey string) *Server {
	return &Server{
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for LiveSplit One compatibility
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		clients: make(map[*Client]bool),
		host:    host,
		port:    port,
		tlsCert: tlsCert,
		tlsKey:  tlsKey,
	}
}

// SetEngine sets the splitting engine for the server
func (s *Server) SetEngine(engine *engine.SplittingEngine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.engine = engine
}

// Start starts the WebSocket server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.host, s.port),
		Handler: mux,
	}

	s.running = true

	useTLS := s.tlsCert != "" && s.tlsKey != ""

	// Start server in goroutine
	go func() {
		var err error
		if useTLS {
			s.logger.WithField("addr", s.server.Addr).Info("Starting LiveSplit One WebSocket server (TLS)")
			err = s.server.ListenAndServeTLS(s.tlsCert, s.tlsKey)
		} else {
			s.logger.WithField("addr", s.server.Addr).Info("Starting LiveSplit One WebSocket server")
			err = s.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("WebSocket server error")
		}
	}()

	// Start monitoring engine events if engine is available
	if s.engine != nil {
		go s.monitorEngineEvents(ctx)
	}

	scheme := "ws"
	if useTLS {
		scheme = "wss"
	}
	s.logger.WithField("addr", fmt.Sprintf("%s://%s", scheme, s.server.Addr)).Info("LiveSplit One WebSocket server started")
	return nil
}

// Stop stops the WebSocket server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.logger.Info("Stopping LiveSplit One WebSocket server")

	// Close all client connections
	for client := range s.clients {
		client.close()
	}

	// Stop HTTP server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.WithError(err).Error("Error shutting down WebSocket server")
			return err
		}
	}

	s.running = false
	s.logger.Info("LiveSplit One WebSocket server stopped")
	return nil
}

// IsRunning returns whether the server is running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetClientCount returns the number of connected clients
func (s *Server) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// handleWebSocket handles WebSocket connection upgrades
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.WithError(err).Error("Failed to upgrade WebSocket connection")
		return
	}

	client := &Client{
		conn:   conn,
		server: s,
		send:   make(chan []byte, 256),
		logger: s.logger.WithFields(logrus.Fields{
			"client": conn.RemoteAddr().String(),
		}),
	}

	s.registerClient(client)

	// Start client goroutines
	go client.writePump()
	go client.readPump()
}

// registerClient registers a new client
func (s *Server) registerClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[client] = true
	client.logger.Info("LiveSplit One client connected")
}

// unregisterClient unregisters a client
func (s *Server) unregisterClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.clients[client]; ok {
		delete(s.clients, client)
		close(client.send)
		client.logger.Info("LiveSplit One client disconnected")
	}
}

// broadcast sends a message to all connected clients
func (s *Server) broadcast(message []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		select {
		case client.send <- message:
		default:
			close(client.send)
			delete(s.clients, client)
		}
	}
}

// monitorEngineEvents monitors splitting engine events and sends them to clients
func (s *Server) monitorEngineEvents(ctx context.Context) {
	if s.engine == nil {
		return
	}

	splitChan := s.engine.RegisterSplitChannel(ctx)
	statusChan := s.engine.RegisterStatusChannel(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case splitEvent, ok := <-splitChan:
			if !ok {
				return
			}
			s.handleSplitEvent(splitEvent)
		case statusEvent, ok := <-statusChan:
			if !ok {
				return
			}
			s.handleStatusEvent(statusEvent)
		}
	}
}

// handleSplitEvent handles split events from the engine
func (s *Server) handleSplitEvent(event engine.SplitEvent) {
	if event.Action == engine.SplitActionSplit {
		s.logger.WithField("split", event.SplitName).Info("Broadcasting split to LiveSplit clients")
		// Send split command
		s.sendCommand("split")
	}
}

// handleStatusEvent handles status events from the engine
func (s *Server) handleStatusEvent(event engine.StatusEvent) {
	switch event.State {
	case engine.StateRunning:
		s.sendCommand("start")
	}
}

// sendCommand sends a command to all connected clients
func (s *Server) sendCommand(command string) {
	cmd := Command{
		Command: command,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		s.logger.WithError(err).Error("Failed to marshal command")
		return
	}

	s.broadcast(data)
	s.logger.WithField("command", command).Info("Command sent to LiveSplit clients")
}

// Client methods

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.server.unregisterClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.WithError(err).Error("WebSocket read error")
			}
			break
		}

		// Handle incoming commands from LiveSplit One
		c.handleIncomingMessage(message)
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				c.logger.WithError(err).Error("WebSocket write error")
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleIncomingMessage handles event messages received from LiveSplit One
// LiveSplit One doesn't send us commands, only events. These events could reflect
// LiveSplit catching up with commands we sent, or they could reflect something the
// user did in LiveSplit. We should always assume that split events correspond to
// commands we sent, and that skip/undo events are user-initiated via LiveSplit.
func (c *Client) handleIncomingMessage(message []byte) {
	c.logger.WithField("message", string(message)).Info("Received message from LiveSplit client")

	// Parse the event
	var cmd map[string]interface{}
	if err := json.Unmarshal(message, &cmd); err != nil {
		c.logger.WithError(err).Error("Failed to parse incoming message")
		return
	}

	// Handle different command types
	if event, ok := cmd["event"].(string); ok {
		switch event {
		case "Started":
			c.logger.Info("Started event received from LiveSplit client")
			c.handleStartedEvent()
		case "Reset":
			c.logger.Info("Reset event received from LiveSplit client")
			c.handleResetEvent()
		case "SplitSkipped":
			c.logger.Info("Split skipped event received from LiveSplit client")
			c.handleSplitSkippedEvent()
		case "SplitUndone":
			c.logger.Info("Split undone event received from LiveSplit client")
			c.handleSplitUndoneEvent()
		default:
			c.logger.WithField("event", event).Info("Unhandled event from LiveSplit client")
		}
	}
}

// handleStartedEvent handles started events from LiveSplit One. This lets the
// LiveSplit timer starting move the engine past autostart into the running
// state so it begins evaluating the first split's condition. The engine's own
// "start" command is broadcast to all clients (including the sender) via the
// status event monitor, so no explicit forwarding is done here.
func (c *Client) handleStartedEvent() {
	if c.server.engine != nil {
		if err := c.server.engine.ManualStart(); err != nil {
			c.logger.WithError(err).Error("Failed to start engine")
			return
		}
		c.logger.Info("Run started by LiveSplit client")
	}
}

// handleResetEvent handles reset events from LiveSplit One
func (c *Client) handleResetEvent() {
	if c.server.engine != nil {
		if err := c.server.engine.ManualReset(); err != nil {
			c.logger.WithError(err).Error("Failed to reset run")
		} else {
			c.logger.Info("Run reset by LiveSplit client")
		}
	}
}

// handleSplitSkippedEvent handles split skipped events from LiveSplit One
func (c *Client) handleSplitSkippedEvent() {
	if c.server.engine != nil {
		if err := c.server.engine.ManualSkipSplit(); err != nil {
			c.logger.WithError(err).Error("Failed to skip split")
		}
	}
}

// handleSplitUndoneEvent handles split undone events from LiveSplit One
func (c *Client) handleSplitUndoneEvent() {
	if c.server.engine != nil {
		if err := c.server.engine.ManualUndoSplit(); err != nil {
			c.logger.WithError(err).Error("Failed to undo split")
		}
	}
}

// close closes the client connection
func (c *Client) close() {
	c.conn.Close()
}
