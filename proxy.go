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

type ProxyManager struct {
	config    *ProxyConfig
	auth      *proxy.Auth
	dialer    proxy.Dialer
	transport *http.Transport
	enabled   bool
}

func NewProxyManager(cfg *ProxyConfig) (*ProxyManager, error) {
	if !cfg.Enabled {
		log.Println("[proxy] Proxy disabled — using direct connections")
		return &ProxyManager{config: cfg, enabled: false}, nil
	}

	proxyAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	auth := proxy.Auth{
		User:     cfg.Username,
		Password: cfg.Password,
	}

	baseDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, &auth, baseDialer)
	if err != nil {
		return nil, fmt.Errorf("proxy: failed to create SOCKS5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		},
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   0,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     false,
	}

	log.Printf("[proxy] Webshare SOCKS5 proxy configured: %s (user: %s)", proxyAddr, cfg.Username)

	return &ProxyManager{
		config:    cfg,
		auth:      &auth,
		dialer:    socksDialer,
		transport: transport,
		enabled:   true,
	}, nil
}

func (pm *ProxyManager) NewClient() *http.Client {
	if !pm.enabled || pm.transport == nil {
		return &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxConnsPerHost: 0,
				IdleConnTimeout: 90 * time.Second,
			},
		}
	}

	transport := pm.transport.Clone()
	transport.DisableKeepAlives = false

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

func (pm *ProxyManager) NewClientWithTimeout(timeout time.Duration) *http.Client {
	client := pm.NewClient()
	client.Timeout = timeout
	return client
}

func ParseTarget(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("target URL is empty")
	}
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
