package main

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	ListenPort          string   `yaml:"listen_port"`
	LogLevel            string   `yaml:"log_level"`
	SoundEffectFilePath string   `yaml:"sound_effect_file_path"`
	WebhookAPIKey       string   `yaml:"webhook_api_key"` // API key for webhook authentication (optional)
	AllowedIPs          []string `yaml:"allowed_ips"`     // IP whitelist (optional, empty = allow all)
	RequireHTTPS        bool     `yaml:"require_https"`   // Require HTTPS (optional, default: false)
}

func ParseConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	err = yaml.NewDecoder(file).Decode(config)
	if err != nil {
		return nil, err
	}
	return config, nil
}
