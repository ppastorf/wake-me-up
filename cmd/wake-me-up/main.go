package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

var configPath = flag.String("config", "config/config.yaml", "Path to config.yaml.")

func main() {
	flag.Parse()

	config, err := ParseConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse config: %v\n", err)
		os.Exit(1)
	}

	err = InitLogger(config.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	log.Infof("Starting Wake Me Up")
	log.Infof("Config file '%s' loaded successfully", *configPath)
	log.Debugf("Parsed config: %+v", config)

	AppState := NewAppState(100)
	AppState.config = config

	// Apply authentication middleware to webhook endpoint if configured
	webhookHandlerFunc := webhookHandler(AppState)
	if config.WebhookAPIKey != "" || len(config.AllowedIPs) > 0 || config.RequireHTTPS {
		webhookHandlerFunc = authMiddleware(config, webhookHandlerFunc)
		log.Infof("Webhook authentication enabled (API Key: %v, IP Whitelist: %v, Require HTTPS: %v)",
			config.WebhookAPIKey != "", len(config.AllowedIPs) > 0, config.RequireHTTPS)
	}

	http.HandleFunc("/webhook", webhookHandlerFunc)
	http.HandleFunc("/acknowledge", acknowledgeHandler(AppState))
	http.HandleFunc("/clear", clearHandler(AppState))
	http.HandleFunc("/sound", soundHandler(AppState))
	http.HandleFunc("/status", statusHandler(AppState))
	http.HandleFunc("/ws", wsHandler(AppState))
	http.HandleFunc("/", indexHandler(AppState))

	log.Infof("Starting server on port %s", config.ListenPort)
	if err := http.ListenAndServe(":"+config.ListenPort, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
