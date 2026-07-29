package main

import (
	"os"
	"strconv"
	"time"
)

// ProxyConfig holds Webshare rotating proxy credentials.
type ProxyConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
}

// DefaultConfig returns the hardcoded configuration.
// Overridable via environment variables.
func DefaultConfig() *Config {
	proxy := &ProxyConfig{
		Enabled:  true,
		Host:     "p.webshare.io",
		Port:     80,
		Username: "wwwsyxzg-rotate",
		Password: "582ygxexguhx",
	}

	// Allow override from environment
	if v := os.Getenv("PROXY_ENABLED"); v == "false" || v == "0" {
		proxy.Enabled = false
	}
	if v := os.Getenv("PROXY_HOST"); v != "" {
		proxy.Host = v
	}
	if v := os.Getenv("PROXY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			proxy.Port = p
		}
	}
	if v := os.Getenv("PROXY_USER"); v != "" {
		proxy.Username = v
	}
	if v := os.Getenv("PROXY_PASS"); v != "" {
		proxy.Password = v
	}

	port := 8080
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}

	return &Config{
		Port:          port,
		DefaultDuration: 300 * time.Second,
		Proxy:           proxy,
		MaxConcurrency:  3000,
		// Default per-layer workers (sum <= MaxConcurrency)
		DefaultLayers: LayerWorkers{
			L1: 800,  // Chunked transfer abuse
			L2: 1000, // Recursive params
			L3: 600,  // Cache bypass
			L4: 400,  // Connection pool exhaustion
			L5: 200,  // Parser stress (headers)
		},
	}
}

// Config holds all runtime configuration.
type Config struct {
	Port            int
	DefaultDuration time.Duration
	Proxy           *ProxyConfig
	MaxConcurrency  int
	DefaultLayers   LayerWorkers
}

// LayerWorkers defines worker counts per attack layer.
type LayerWorkers struct {
	L1 int // Chunked transfer encoding
	L2 int // Recursive parameter requests
	L3 int // Cache busting + unique URLs
	L4 int // Keep-alive connection pool
	L5 int // Oversized header attacks
}

// Total returns the sum of all layer workers.
func (lw LayerWorkers) Total() int {
	return lw.L1 + lw.L2 + lw.L3 + lw.L4 + lw.L5
}
