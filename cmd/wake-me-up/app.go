package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type AppState struct {
	mu           sync.RWMutex
	alerts       []AlertEntry
	maxSize      int
	config       *Config
	acknowledged map[string]bool // alert ID -> acknowledged
	hub          *Hub            // WebSocket hub for real-time updates
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients
	broadcast chan []byte

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client
}

// Client is a middleman between the websocket connection and the hub
type Client struct {
	hub *Hub

	// The websocket connection
	conn *websocket.Conn

	// Buffered channel of outbound messages
	send chan []byte
}

// UpdateMessage represents a message sent over WebSocket
type UpdateMessage struct {
	Type              string              `json:"type"`
	Alerts            []AlertEntryWithAck `json:"alerts,omitempty"`
	HasUnacknowledged bool                `json:"hasUnacknowledged,omitempty"`
}

// AlertEntryWithAck includes the acknowledged status
type AlertEntryWithAck struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Alert          Alert     `json:"alert"`
	IsAcknowledged bool      `json:"isAcknowledged"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
}

func NewAppState(maxSize int) *AppState {
	hub := newHub()
	go hub.run()

	return &AppState{
		alerts:       make([]AlertEntry, 0),
		maxSize:      maxSize,
		acknowledged: make(map[string]bool),
		hub:          hub,
	}
}

// newHub creates a new Hub
func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

// run starts the hub's main loop
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

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

// broadcastUpdate sends an update to all connected clients
func (a *AppState) broadcastUpdate() {
	a.mu.RLock()
	alerts := make([]AlertEntry, len(a.alerts))
	copy(alerts, a.alerts)
	hasUnacknowledged := a.HasUnacknowledgedAlerts()
	acknowledged := make(map[string]bool)
	for k, v := range a.acknowledged {
		acknowledged[k] = v
	}
	a.mu.RUnlock()

	// Convert to AlertEntryWithAck format
	alertsWithAck := make([]AlertEntryWithAck, len(alerts))
	for i, entry := range alerts {
		alertsWithAck[i] = AlertEntryWithAck{
			ID:             entry.ID,
			Timestamp:      entry.Timestamp,
			Alert:          entry.Alert,
			IsAcknowledged: acknowledged[entry.ID],
		}
	}

	// Sort alerts: firing first, then acknowledged, then resolved
	sort.Slice(alertsWithAck, func(i, j int) bool {
		iEntry := alertsWithAck[i]
		jEntry := alertsWithAck[j]

		iPriority := getAlertPriority(iEntry.Alert.Status, iEntry.IsAcknowledged)
		jPriority := getAlertPriority(jEntry.Alert.Status, jEntry.IsAcknowledged)

		if iPriority != jPriority {
			return iPriority < jPriority
		}

		return iEntry.Timestamp.After(jEntry.Timestamp)
	})

	message := UpdateMessage{
		Type:              "update",
		Alerts:            alertsWithAck,
		HasUnacknowledged: hasUnacknowledged,
	}

	jsonData, err := json.Marshal(message)
	if err != nil {
		log.Errorf("Error marshaling update message: %v", err)
		return
	}

	select {
	case a.hub.broadcast <- jsonData:
	default:
		// Non-blocking send
	}
}

// readPump pumps messages from the websocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Errorf("WebSocket error: %v", err)
			}
			break
		}
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
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

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWebSocket handles websocket requests from clients
func serveWebSocket(hub *Hub, state *AppState, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	// Send initial state (will be sent via broadcastUpdate in a moment)

	// Start client pumps
	go client.writePump()
	go client.readPump()

	// Send initial state after client is registered
	go func() {
		time.Sleep(100 * time.Millisecond) // Small delay to ensure client is registered
		state.broadcastUpdate()
	}()
}

func (a *AppState) AddWebhook(payload WebhookPayload) {
	a.mu.Lock()
	timestamp := time.Now()
	baseID := timestamp.UnixNano()

	// Track which resolved alerts actually matched and removed firing alerts
	matchedResolvedAlerts := make([]Alert, 0)

	// If this webhook contains resolved alerts, remove matching firing alerts
	if payload.Status == "resolved" || hasResolvedAlerts(payload.Alerts) {
		matchedResolvedAlerts = a.removeMatchingFiringAlerts(payload.Alerts)
	}

	// Extract each alert and store it individually
	// For resolved alerts, only add them if they matched a firing alert
	for i, alert := range payload.Alerts {
		// Skip resolved alerts that didn't match any firing alert
		if alert.Status == "resolved" {
			// Check if this resolved alert matched a firing alert
			matched := false
			for _, matchedAlert := range matchedResolvedAlerts {
				if alertsMatch(alert, matchedAlert) {
					matched = true
					break
				}
			}
			// If it didn't match, skip it
			if !matched {
				log.Debugf("Ignoring resolved alert that didn't match any firing alert: %v", alert.Labels)
				continue
			}
		}

		alertEntry := AlertEntry{
			ID:        fmt.Sprintf("%d-%d", baseID, i),
			Timestamp: timestamp,
			Alert:     alert,
		}
		a.alerts = append([]AlertEntry{alertEntry}, a.alerts...)
	}

	// Keep only the most recent alerts
	if len(a.alerts) > a.maxSize {
		a.alerts = a.alerts[:a.maxSize]
	}
	a.mu.Unlock()

	// Broadcast update to all WebSocket clients
	a.broadcastUpdate()
}

// hasResolvedAlerts checks if any alerts in the payload are resolved
func hasResolvedAlerts(alerts []Alert) bool {
	for _, alert := range alerts {
		if alert.Status == "resolved" {
			return true
		}
	}
	return false
}

// removeMatchingFiringAlerts removes matching firing alerts and returns the resolved alerts that actually matched
// This should be called while holding the lock
func (a *AppState) removeMatchingFiringAlerts(resolvedAlerts []Alert) []Alert {
	// Extract only resolved alerts
	var resolved []Alert
	for _, alert := range resolvedAlerts {
		if alert.Status == "resolved" {
			resolved = append(resolved, alert)
		}
	}

	if len(resolved) == 0 {
		return []Alert{}
	}

	// Track which resolved alerts actually matched firing alerts
	matchedResolvedAlerts := make([]Alert, 0)

	// Filter out alerts that match resolved alerts
	var filtered []AlertEntry
	for _, entry := range a.alerts {
		shouldRemove := false
		var matchedResolvedAlert Alert

		// Only remove firing alerts that match resolved alerts
		if entry.Alert.Status == "firing" {
			for _, resolvedAlert := range resolved {
				if alertsMatch(resolvedAlert, entry.Alert) {
					shouldRemove = true
					matchedResolvedAlert = resolvedAlert
					log.Debugf("Removing firing alert %s - matches resolved alert with labels: %v", entry.ID, resolvedAlert.Labels)
					break
				}
			}
		}

		// Keep the alert if it shouldn't be removed
		if !shouldRemove {
			filtered = append(filtered, entry)
		} else {
			// Also remove from acknowledged map if present
			delete(a.acknowledged, entry.ID)
			// Track that this resolved alert matched
			matchedResolvedAlerts = append(matchedResolvedAlerts, matchedResolvedAlert)
		}
	}

	a.alerts = filtered
	return matchedResolvedAlerts
}

func (a *AppState) GetAlerts() []AlertEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]AlertEntry, len(a.alerts))
	copy(result, a.alerts)

	// Sort alerts: firing first, then acknowledged, then resolved
	sort.Slice(result, func(i, j int) bool {
		iEntry := result[i]
		jEntry := result[j]

		iAcknowledged := a.acknowledged[iEntry.ID]
		jAcknowledged := a.acknowledged[jEntry.ID]

		// Get priority: firing=0, acknowledged=1, resolved=2
		iPriority := getAlertPriority(iEntry.Alert.Status, iAcknowledged)
		jPriority := getAlertPriority(jEntry.Alert.Status, jAcknowledged)

		if iPriority != jPriority {
			return iPriority < jPriority
		}

		// If same priority, sort by timestamp (newest first)
		return iEntry.Timestamp.After(jEntry.Timestamp)
	})

	return result
}

// getAlertPriority returns a numeric priority for sorting
// Lower number = higher priority (shown first)
func getAlertPriority(status string, acknowledged bool) int {
	if status == "firing" && !acknowledged {
		return 0 // Firing (unacknowledged) - highest priority
	}
	if status == "firing" && acknowledged {
		return 1 // Acknowledged - middle priority
	}
	return 2 // Resolved - lowest priority
}

func (a *AppState) HasUnacknowledgedAlerts() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, entry := range a.alerts {
		if a.acknowledged[entry.ID] {
			continue
		}
		if entry.Alert.Status == "firing" {
			return true
		}
	}
	return false
}

func (a *AppState) IsAcknowledged(alertID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.acknowledged[alertID]
}

func (a *AppState) Acknowledge(alertID string) {
	a.mu.Lock()
	a.acknowledged[alertID] = true
	a.mu.Unlock()
	log.Infof("Alert %s acknowledged", alertID)

	// Broadcast update to all WebSocket clients
	a.broadcastUpdate()
}

func (a *AppState) ClearAcknowledgedAndResolved() int {
	a.mu.Lock()

	var filtered []AlertEntry
	clearedCount := 0

	for _, entry := range a.alerts {
		isAcknowledged := a.acknowledged[entry.ID]

		// Keep only firing alerts that are not acknowledged
		if entry.Alert.Status == "firing" && !isAcknowledged {
			filtered = append(filtered, entry)
		} else {
			// Remove acknowledged or resolved alerts
			delete(a.acknowledged, entry.ID)
			clearedCount++
		}
	}

	a.alerts = filtered
	log.Debugf("Cleared %d acknowledged/resolved alerts", clearedCount)
	a.mu.Unlock()

	// Broadcast update to all WebSocket clients
	a.broadcastUpdate()

	return clearedCount
}

// soundHandler serves the sound file
func soundHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		soundPath := state.config.SoundEffectFilePath
		// Convert relative path to absolute if needed
		if !filepath.IsAbs(soundPath) {
			wd, err := os.Getwd()
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to get working directory: %v", err), http.StatusInternalServerError)
				return
			}
			soundPath = filepath.Join(wd, soundPath)
		}

		// Check if file exists
		if _, err := os.Stat(soundPath); os.IsNotExist(err) {
			http.Error(w, fmt.Sprintf("Sound file not found: %s", soundPath), http.StatusNotFound)
			return
		}

		// Set content type based on file extension
		ext := filepath.Ext(soundPath)
		switch ext {
		case ".wav":
			w.Header().Set("Content-Type", "audio/wav")
		case ".mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
		case ".ogg":
			w.Header().Set("Content-Type", "audio/ogg")
		default:
			w.Header().Set("Content-Type", "audio/wav")
		}

		http.ServeFile(w, r, soundPath)
	}
}

// statusHandler returns the current alert status as JSON
func statusHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		hasUnacknowledged := state.HasUnacknowledgedAlerts()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"hasUnacknowledged": hasUnacknowledged,
		})
	}
}

func webhookHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload WebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
			return
		}

		state.AddWebhook(payload)
		log.Infof("Received webhook: %d alerts, status: %s from IP: %s", len(payload.Alerts), payload.Status, getClientIP(r))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

func acknowledgeHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		alertID := r.URL.Query().Get("id")
		if alertID == "" {
			http.Error(w, "Missing 'id' parameter", http.StatusBadRequest)
			return
		}

		state.Acknowledge(alertID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

func clearHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		clearedCount := state.ClearAcknowledgedAndResolved()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Cleared %d alerts", clearedCount)))
	}
}

func wsHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveWebSocket(state.hub, state, w, r)
	}
}

// TemplateData holds the data for rendering the index template
type TemplateData struct {
	StatusClass string
	StatusText  string
	Alerts      []AlertTemplateData
}

// AlertTemplateData holds data for a single alert in the template
type AlertTemplateData struct {
	ID            string
	Timestamp     string
	StatusClass   string
	StatusText    string
	ShowAckButton bool
	AlertName     string
	Labels        []LabelData
	StartsAt      string
	EndsAt        string
}

// LabelData holds label key-value pairs for the template
type LabelData struct {
	Key   string
	Value string
}

func indexHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alerts := state.GetAlerts()
		hasUnacknowledged := state.HasUnacknowledgedAlerts()

		// Prepare template data
		templateData := TemplateData{
			StatusClass: getStatusClass(hasUnacknowledged),
			StatusText:  getStatusText(hasUnacknowledged),
			Alerts:      make([]AlertTemplateData, 0),
		}

		// Convert alerts to template data
		for _, entry := range alerts {
			isAcknowledged := state.IsAcknowledged(entry.ID)
			alert := entry.Alert

			// Determine status class and text
			statusClass := "resolved"
			statusText := "Resolved"

			if alert.Status == "firing" {
				if isAcknowledged {
					statusClass = "acknowledged"
					statusText = "Acknowledged"
				} else {
					statusClass = "firing"
					statusText = "Firing"
				}
			} else if alert.Status == "resolved" {
				statusClass = "resolved"
				statusText = "Resolved"
			}

			// Extract alertname if it exists
			alertName := ""
			if name, exists := alert.Labels["alertname"]; exists {
				alertName = name
			}

			// Prepare labels
			labels := make([]LabelData, 0)
			if len(alert.Labels) > 0 {
				labelKeys := make([]string, 0, len(alert.Labels))
				for k := range alert.Labels {
					labelKeys = append(labelKeys, k)
				}
				sort.Strings(labelKeys)
				for _, k := range labelKeys {
					labels = append(labels, LabelData{Key: k, Value: alert.Labels[k]})
				}
			}

			// Format timestamps
			endsAt := ""
			if alert.EndsAt != nil {
				endsAt = alert.EndsAt.Format("2006-01-02 15:04:05")
			}

			alertData := AlertTemplateData{
				ID:            entry.ID,
				Timestamp:     entry.Timestamp.Format("2006-01-02 15:04:05"),
				StatusClass:   statusClass,
				StatusText:    statusText,
				ShowAckButton: alert.Status == "firing" && !isAcknowledged,
				AlertName:     alertName,
				Labels:        labels,
				StartsAt:      alert.StartsAt.Format("2006-01-02 15:04:05"),
				EndsAt:        endsAt,
			}

			templateData.Alerts = append(templateData.Alerts, alertData)
		}

		// Parse and execute template
		// Resolve template path relative to working directory
		wd, err := os.Getwd()
		if err != nil {
			log.Errorf("Error getting working directory: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		templatePath := filepath.Join(wd, "templates", "index.html")
		tmpl, err := template.ParseFiles(templatePath)
		if err != nil {
			log.Errorf("Error parsing template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.Execute(w, templateData); err != nil {
			log.Errorf("Error executing template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
}
