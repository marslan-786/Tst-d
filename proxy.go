package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyManager creates HTTP clients that route through Webshare rotating proxy.
// Every new connection gets a different exit IP automatically.
type ProxyManager struct {
	config    *ProxyConfig
	auth      *proxy.Auth
	dialer    proxy.Dialer
	transport *http.Transport
}

// NewProxyManager initializes the proxy manager.
func NewProxyManager(cfg *ProxyConfig) (*ProxyManager, error) {
	if !cfg.Enabled {
		log.Println("[proxy] Proxy disabled — using direct connections")
		return &ProxyManager{config: cfg}, nil
	}

	proxyAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// SOCKS5 authentication
	auth := proxy.Auth{
		User:     cfg.Username,
		Password: cfg.Password,
	}

	// Create SOCKS5 dialer
	baseDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, &auth, baseDialer)
	if err != nil {
		return nil, fmt.Errorf("proxy: failed to create SOCKS5 dialer: %w", err)
	}

	// Build HTTP transport that uses the SOCKS5 dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		},
		MaxIdleConns:          0,    // no limit
		MaxIdleConnsPerHost:   0,    // no limit — we need every connection
		MaxConnsPerHost:       0,    // no limit
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     false, // keep alive for connection reuse
	}

	log.Printf("[proxy] Webshare SOCKS5 proxy configured: %s (user: %s)", proxyAddr, cfg.Username)

	return &ProxyManager{
		config:    cfg,
		auth:      &auth,
		dialer:    socksDialer,
		transport: transport,
	}, nil
}

// NewClient returns an http.Client that routes through the rotating proxy.
// Call this once per connection to ensure IP rotation.
func (pm *ProxyManager) NewClient() *http.Client {
	if !pm.config.Enabled || pm.transport == nil {
		return &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxConnsPerHost: 0,
				IdleConnTimeout: 90 * time.Second,
			},
		}
	}

	// Create a fresh transport clone to avoid connection reuse across IPs
	// This ensures Webshare assigns a new exit IP for each new connection
	transport := pm.transport.Clone()
	transport.DisableKeepAlives = false // allow keep-alive per-connection

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// NewClientWithTimeout returns a client with a custom timeout.
func (pm *ProxyManager) NewClientWithTimeout(timeout time.Duration) *http.Client {
	client := pm.NewClient()
	client.Timeout = timeout
	return client
}

// ParseTarget validates and normalizes the target URL.
func ParseTarget(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("target URL is empty")
	}
	// Add scheme if missing
	if !hasScheme(raw) {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s (only http/https allowed)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("target URL has no host")
	}
	return u, nil
}

func hasScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i > 0 && i+2 < len(s) && s[i+1] == '/' && s[i+2] == '/'
		}
		if s[i] == '/' {
			return false
		}
	}
	return false
}
