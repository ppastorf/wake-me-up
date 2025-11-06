package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type AppState struct {
	mu              sync.RWMutex
	alerts          []AlertEntry
	maxSize         int
	config          *Config
	acknowledged    map[string]bool // alert ID -> acknowledged
	soundPlaying    bool
	soundStopChan   chan struct{}
	currentSoundCmd *exec.Cmd // current playing sound command
}

func NewAppState(maxSize int) *AppState {
	return &AppState{
		alerts:        make([]AlertEntry, 0),
		maxSize:       maxSize,
		acknowledged:  make(map[string]bool),
		soundStopChan: make(chan struct{}),
	}
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

	// Check if there are unacknowledged firing alerts and start/continue sound loop
	a.checkAndPlaySound()
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
					log.Infof("Removing firing alert %s - matches resolved alert with labels: %v", entry.ID, resolvedAlert.Labels)
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

	// Stop sound if no more unacknowledged alerts
	a.checkAndPlaySound()
	log.Infof("Alert %s acknowledged", alertID)
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
	log.Infof("Cleared %d acknowledged/resolved alerts", clearedCount)
	a.mu.Unlock()

	// Stop sound if no more unacknowledged alerts
	a.checkAndPlaySound()

	return clearedCount
}

// startSound starts playing a sound and returns the command so it can be killed
func (a *AppState) startSound(soundFilePath string) *exec.Cmd {
	var cmd *exec.Cmd

	if _, err := exec.LookPath("afplay"); err == nil {
		// macOS
		cmd = exec.Command("afplay", soundFilePath)
	} else if _, err := exec.LookPath("paplay"); err == nil {
		// Linux with PulseAudio
		cmd = exec.Command("paplay", soundFilePath)
	} else if _, err := exec.LookPath("aplay"); err == nil {
		// Linux with ALSA
		cmd = exec.Command("aplay", soundFilePath)
	} else {
		// Fallback: use system beep (can't be killed)
		fmt.Print("\a")
		return nil
	}

	if cmd != nil {
		// Start the command but don't wait for it
		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start sound: %v", err)
			return nil
		}
	}

	return cmd
}

func (a *AppState) checkAndPlaySound() {
	hasUnacknowledged := a.HasUnacknowledgedAlerts()

	a.mu.Lock()
	shouldPlay := hasUnacknowledged && !a.soundPlaying
	if !hasUnacknowledged && a.soundPlaying {
		// Stop the sound loop immediately
		close(a.soundStopChan)
		a.soundStopChan = make(chan struct{})

		// Kill any currently playing sound command
		if a.currentSoundCmd != nil && a.currentSoundCmd.Process != nil {
			if err := a.currentSoundCmd.Process.Kill(); err != nil {
				log.Debugf("Error killing sound process: %v", err)
			}
			a.currentSoundCmd = nil
		}

		a.soundPlaying = false
	}
	a.mu.Unlock()

	if shouldPlay {
		go a.playSoundLoop()
	}
}

func (a *AppState) playSoundLoop() {
	a.mu.Lock()
	if a.soundPlaying {
		a.mu.Unlock()
		return
	}
	a.soundPlaying = true
	stopChan := a.soundStopChan
	a.mu.Unlock()

	log.Infof("Starting continuous sound loop for unacknowledged alerts")

	for {
		// Check if we should stop
		select {
		case <-stopChan:
			log.Infof("Stopping sound loop")
			return
		default:
		}

		// Check if there are still unacknowledged alerts
		if !a.HasUnacknowledgedAlerts() {
			a.mu.Lock()
			a.soundPlaying = false
			if a.currentSoundCmd != nil && a.currentSoundCmd.Process != nil {
				a.currentSoundCmd.Process.Kill()
				a.currentSoundCmd = nil
			}
			a.mu.Unlock()
			return
		}

		// Play sound and store the command so it can be killed
		cmd := a.startSound(a.config.SoundEffectFilePath)
		if cmd != nil {
			a.mu.Lock()
			a.currentSoundCmd = cmd
			a.mu.Unlock()
		}

		// Wait for sound to finish or stop signal
		soundDone := make(chan error, 1)
		go func() {
			if cmd != nil {
				soundDone <- cmd.Wait()
			} else {
				soundDone <- nil
			}
		}()

		select {
		case <-stopChan:
			// Stop signal received, kill the sound
			a.mu.Lock()
			if a.currentSoundCmd != nil && a.currentSoundCmd.Process != nil {
				a.currentSoundCmd.Process.Kill()
				a.currentSoundCmd = nil
			}
			a.mu.Unlock()
			log.Infof("Stopping sound loop")
			return
		case <-soundDone:
			// Sound finished playing
			a.mu.Lock()
			a.currentSoundCmd = nil
			a.mu.Unlock()
		}

		// Wait a bit before playing again, but check for stop signal
		select {
		case <-stopChan:
			log.Infof("Stopping sound loop")
			return
		case <-time.After(2 * time.Second):
			// Continue loop
		}
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

func indexHandler(state *AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alerts := state.GetAlerts()
		hasUnacknowledged := state.HasUnacknowledgedAlerts()

		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Wake me Up!</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        .header {
            background: white;
            padding: 30px;
            border-radius: 10px;
            box-shadow: 0 10px 30px rgba(0,0,0,0.2);
            margin-bottom: 20px;
        }
        h1 {
            color: #333;
            margin-bottom: 10px;
        }
        .status {
            display: inline-block;
            padding: 8px 16px;
            border-radius: 20px;
            font-weight: bold;
            margin-top: 10px;
        }
        .status.active {
            background: #ff4444;
            color: white;
            animation: pulse 2s infinite;
        }
        .status.clear {
            background: #44ff44;
            color: white;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.7; }
        }
        .alert-list {
            display: grid;
            gap: 20px;
        }
        .alert-card {
            background: white;
            padding: 20px;
            border-radius: 10px;
            box-shadow: 0 5px 15px rgba(0,0,0,0.1);
        }
        .alert-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 15px;
            padding-bottom: 15px;
            border-bottom: 2px solid #f0f0f0;
        }
        .alert-id {
            font-size: 12px;
            color: #666;
        }
        .alert-time {
            font-size: 14px;
            color: #999;
        }
        .alert-item {
            background: #f8f9fa;
            padding: 15px;
            margin: 10px 0;
            border-radius: 5px;
            border-left: 4px solid #667eea;
        }
        .alert-item.firing {
            border-left-color: #ff4444;
            background: #fff5f5;
        }
        .alert-item.resolved {
            border-left-color: #44ff44;
            background: #f0fff4;
        }
        .alert-item.acknowledged {
            border-left-color: #ffc107;
            background: #fffbf0;
        }
        .alert-status {
            display: inline-block;
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 12px;
            font-weight: bold;
            margin-bottom: 8px;
        }
        .alert-status.firing {
            background: #ff4444;
            color: white;
        }
        .alert-status.resolved {
            background: #44ff44;
            color: white;
        }
        .alert-status.acknowledged {
            background: #ffc107;
            color: #333;
        }
        .label {
            display: inline-block;
            background: #e9ecef;
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 11px;
            margin: 2px;
            font-family: monospace;
        }
        .empty-state {
            text-align: center;
            padding: 60px 20px;
            color: white;
            font-size: 18px;
        }
        .refresh-btn {
            background: #667eea;
            color: white;
            border: none;
            padding: 10px 20px;
            border-radius: 5px;
            cursor: pointer;
            font-size: 14px;
            margin-top: 10px;
        }
        .refresh-btn:hover {
            background: #5568d3;
        }
        .clear-btn {
            background: #9e9e9e;
            color: white;
            border: none;
            padding: 10px 20px;
            border-radius: 5px;
            cursor: pointer;
            font-size: 14px;
            margin-top: 10px;
            margin-left: 10px;
        }
        .clear-btn:hover {
            background: #757575;
        }
        .ack-btn {
            background: #ff9800;
            color: white;
            border: none;
            padding: 8px 16px;
            border-radius: 5px;
            cursor: pointer;
            font-size: 14px;
            margin-top: 10px;
            font-weight: bold;
        }
        .ack-btn:hover {
            background: #f57c00;
        }
        .ack-btn:disabled {
            background: #ccc;
            cursor: not-allowed;
        }
        .acknowledged {
            border: 2px solid #ffc107;
        }
        .ack-status {
            display: inline-block;
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 11px;
            font-weight: bold;
            margin-left: 8px;
            background: #ffc107;
            color: #333;
        }
    </style>
    <script>
        function refreshPage() {
            location.reload();
        }
        function acknowledgeAlert(alertId) {
            fetch('/acknowledge?id=' + alertId, {
                method: 'POST'
            })
            .then(response => {
                if (response.ok) {
                    refreshPage();
                } else {
                    alert('Failed to acknowledge alert');
                }
            })
            .catch(error => {
                console.error('Error:', error);
                alert('Failed to acknowledge alert');
            });
        }
        function clearAlerts() {
            fetch('/clear', {
                method: 'POST'
            })
            .then(response => {
                if (response.ok) {
                    refreshPage();
                } else {
                    alert('Failed to clear alerts');
                }
            })
            .catch(error => {
                console.error('Error:', error);
                alert('Failed to clear alerts');
            });
        }
        // Auto-refresh every 5 seconds
        setInterval(refreshPage, 5000);
    </script>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🚨 Wake me Up!</h1>
            <div class="status ` + getStatusClass(hasUnacknowledged) + `">
                ` + getStatusText(hasUnacknowledged) + `
            </div>
            <button class="refresh-btn" onclick="refreshPage()">Refresh</button>
            <button class="clear-btn" onclick="clearAlerts()">Clear</button>
        </div>
        <div class="alert-list">`

		if len(alerts) == 0 {
			html += `
            <div class="empty-state">
                <h2>No alerts received yet</h2>
                <p>Waiting for Alertmanager to send alerts...</p>
            </div>`
		} else {
			for _, entry := range alerts {
				isAcknowledged := state.IsAcknowledged(entry.ID)
				alert := entry.Alert

				// Determine status class and text
				statusClass := "resolved"
				statusText := alert.Status
				cardClass := ""
				ackStatusHTML := ""

				if alert.Status == "firing" {
					if isAcknowledged {
						statusClass = "acknowledged"
						statusText = "acknowledged"
						cardClass = "acknowledged"
						ackStatusHTML = `<span class="ack-status">✓ ACKNOWLEDGED</span>`
					} else {
						statusClass = "firing"
					}
				} else if alert.Status == "resolved" {
					statusClass = "resolved"
				}

				html += fmt.Sprintf(`
            <div class="alert-card %s">
                <div class="alert-header">
                    <div>
                        <div class="alert-id">ID: %s</div>
                        <div class="alert-time">%s</div>
                    </div>
                    <div>
                        <div class="alert-status %s">%s</div>
                        %s
                    </div>
                </div>`, cardClass, entry.ID, entry.Timestamp.Format("2006-01-02 15:04:05"), statusClass, statusText, ackStatusHTML)

				if alert.Status == "firing" && !isAcknowledged {
					html += fmt.Sprintf(`
                <div style="margin-bottom: 15px;">
                    <button class="ack-btn" onclick="acknowledgeAlert('%s')">
                        ✓ Acknowledge Alert
                    </button>
                </div>`, entry.ID)
				}

				html += fmt.Sprintf(`
                <div class="alert-item %s">`, statusClass)

				if len(alert.Labels) > 0 {
					// Extract alertname if it exists
					alertName := ""
					if name, exists := alert.Labels["alertname"]; exists {
						alertName = name
					}

					// Display alertname on top
					if alertName != "" {
						html += fmt.Sprintf(`<div style="margin: 8px 0;"><span style="font-size: 16px; font-weight: bold; color: #333;">%s</span></div>`, alertName)
					}

					// Display Labels section
					html += `<div style="margin: 8px 0;"><strong>Labels:</strong><br>`

					// Sort labels by key for consistent display
					labelKeys := make([]string, 0, len(alert.Labels))
					for k := range alert.Labels {
						labelKeys = append(labelKeys, k)
					}
					sort.Strings(labelKeys)
					for _, k := range labelKeys {
						html += fmt.Sprintf(`<span class="label">%s=%s</span>`, k, alert.Labels[k])
					}
					html += `</div>`
				}

				html += fmt.Sprintf(`
                    <div style="margin-top: 8px; font-size: 12px; color: #666;">
                        Started: %s
                    </div>`, alert.StartsAt.Format("2006-01-02 15:04:05"))

				if alert.EndsAt != nil {
					html += fmt.Sprintf(`
                    <div style="margin-top: 4px; font-size: 12px; color: #666;">
                        Ended: %s
                    </div>`, alert.EndsAt.Format("2006-01-02 15:04:05"))
				}

				html += `</div></div>`
			}
		}

		html += `
        </div>
    </div>
</body>
</html>`

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}
}
