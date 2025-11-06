package main

import "time"

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       *time.Time        `json:"endsAt,omitempty"`
	GeneratorURL string            `json:"generatorURL"`
}

type AlertGroup struct {
	Labels map[string]string `json:"labels"`
	Alerts []Alert           `json:"alerts"`
}

func getStatusClass(hasUnacknowledged bool) string {
	if hasUnacknowledged {
		return "active"
	}
	return "clear"
}

func getStatusText(hasUnacknowledged bool) string {
	if hasUnacknowledged {
		return "⚠️ UNACKNOWLEDGED ALERTS"
	}
	return "✓ ALL CLEAR"
}

// alertsMatch checks if two alerts have matching labels
// Two alerts match only if ALL labels match exactly (bidirectional check)
// Both alerts must have the same set of labels with the same values
func alertsMatch(resolvedAlert, firingAlert Alert) bool {
	// Both must have labels
	if len(resolvedAlert.Labels) == 0 || len(firingAlert.Labels) == 0 {
		return false
	}

	// Must have the same number of labels
	if len(resolvedAlert.Labels) != len(firingAlert.Labels) {
		return false
	}

	// Check if all labels from the resolved alert match the firing alert
	for key, resolvedValue := range resolvedAlert.Labels {
		firingValue, exists := firingAlert.Labels[key]
		if !exists || firingValue != resolvedValue {
			return false
		}
	}

	// Check if all labels from the firing alert match the resolved alert (bidirectional)
	// Since we already checked length and one direction, this ensures exact match
	for key, firingValue := range firingAlert.Labels {
		resolvedValue, exists := resolvedAlert.Labels[key]
		if !exists || resolvedValue != firingValue {
			return false
		}
	}

	return true
}
