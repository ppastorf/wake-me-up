package main

import (
	"time"
)

// Alertmanager webhook payload structure
type WebhookPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

// AlertEntry represents a single alert with its metadata
type AlertEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Alert     Alert     `json:"alert"`
}
