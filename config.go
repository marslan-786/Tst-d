package main

import (
	"os"
	"strconv"
	"time"
)

type ProxyConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
}

func DefaultConfig() *Config {
	proxy := &ProxyConfig{
		Enabled:  true,
		Host:     "p.webshare.io",
		Port:     80,
		Username: "wwwsyxzg-rotate",
		Password: "582ygxexguhx",
	}

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
		Port:            port,
		DefaultDuration: 0, // unlimited by default
		Proxy:           proxy,
		MaxConcurrency:  40000,
		DefaultLayers: LayerWorkers{
			L1: 3000,
			L2: 5000,
			L3: 4000,
			L4: 26000,
			L5: 2000,
		},
	}
}

type Config struct {
	Port            int
	DefaultDuration time.Duration
	Proxy           *ProxyConfig
	MaxConcurrency  int
	DefaultLayers   LayerWorkers
}

type LayerWorkers struct {
	L1 int
	L2 int
	L3 int
	L4 int
	L5 int
}

func (lw LayerWorkers) Total() int {
	return lw.L1 + lw.L2 + lw.L3 + lw.L4 + lw.L5
}
