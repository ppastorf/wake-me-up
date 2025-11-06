package main

import (
	"net"
	"net/http"
	"strings"
)

// authMiddleware wraps a handler with authentication checks
func authMiddleware(config *Config, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check IP whitelist if configured
		if len(config.AllowedIPs) > 0 {
			clientIP := getClientIP(r)
			if !isIPAllowed(clientIP, config.AllowedIPs) {
				log.Warnf("Rejected webhook from unauthorized IP: %s", clientIP)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		// Check API key if configured
		if config.WebhookAPIKey != "" {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				// Also check Authorization header with Bearer token
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					apiKey = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			if apiKey != config.WebhookAPIKey {
				log.Warnf("Rejected webhook with invalid API key from IP: %s", getClientIP(r))
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Check HTTPS requirement if configured
		if config.RequireHTTPS && r.TLS == nil {
			log.Warnf("Rejected non-HTTPS webhook request from IP: %s", getClientIP(r))
			http.Error(w, "HTTPS required", http.StatusBadRequest)
			return
		}

		// All checks passed, call the handler
		handler(w, r)
	}
}

// getClientIP extracts the client IP from the request
// Handles X-Forwarded-For and X-Real-IP headers for proxies
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for proxies/load balancers)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// isIPAllowed checks if an IP is in the allowed list
// Supports CIDR notation (e.g., "10.0.0.0/8") and exact IPs
func isIPAllowed(clientIP string, allowedIPs []string) bool {
	clientIPParsed := net.ParseIP(clientIP)
	if clientIPParsed == nil {
		return false
	}

	for _, allowed := range allowedIPs {
		allowed = strings.TrimSpace(allowed)
		
		// Check if it's a CIDR notation
		if strings.Contains(allowed, "/") {
			_, ipNet, err := net.ParseCIDR(allowed)
			if err == nil && ipNet.Contains(clientIPParsed) {
				return true
			}
		} else {
			// Exact IP match
			allowedIP := net.ParseIP(allowed)
			if allowedIP != nil && allowedIP.Equal(clientIPParsed) {
				return true
			}
		}
	}

	return false
}

