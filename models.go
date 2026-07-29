package main

import (
	"sync/atomic"
	"time"
)

// LayerConfig is used when starting a new attack.
type LayerConfig struct {
	L1 int `json:"l1"`
	L2 int `json:"l2"`
	L3 int `json:"l3"`
	L4 int `json:"l4"`
	L5 int `json:"l5"`
}

// AttackRequest is the JSON payload from the frontend.
type AttackRequest struct {
	Target   string      `json:"target"`
	Layers   LayerConfig `json:"layers"`
	Duration int         `json:"duration"` // seconds
}

// AttackResponse is returned after a start/stop request.
type AttackResponse struct {
	AttackID string `json:"attack_id,omitempty"`
	Error    string `json:"error,omitempty"`
	Success  bool   `json:"success"`
}

// AttackStatus is returned by the status endpoint.
type AttackStatus struct {
	Active   bool   `json:"active"`
	AttackID string `json:"attack_id"`
	Target   string `json:"target"`
	Uptime   int64  `json:"uptime_ms"` // milliseconds since start
}

// Stats holds all real-time counters (lock-free via atomic).
type Stats struct {
	TotalRequests   atomic.Int64
	SuccessRequests atomic.Int64
	FailedRequests  atomic.Int64
	ActiveWorkers   atomic.Int32
	BytesSent       atomic.Int64
	BytesRecv       atomic.Int64
	StartTime       time.Time
	Layers          *LayerStats
}

// LayerStats holds per-layer metrics.
type LayerStats struct {
	Name          string
	Requests      atomic.Int64
	Success       atomic.Int64
	Fail          atomic.Int64
	ActiveWorkers atomic.Int32
}

// StatsSnapshot is sent to the frontend via WebSocket every 500ms.
type StatsSnapshot struct {
	Type    string          `json:"type"` // "stats"
	RPS     int64           `json:"rps"`
	Total   int64           `json:"total"`
	Success int64           `json:"success"`
	Fail    int64           `json:"fail"`
	Active  int32           `json:"active"`
	Uptime  int64           `json:"uptime"` // milliseconds
	Layers  []LayerSnapshot `json:"layers,omitempty"`
}

// LayerSnapshot is the per-layer breakdown for the frontend.
type LayerSnapshot struct {
	Name    string `json:"name"`
	Req     int64  `json:"req"`
	Success int64  `json:"success"`
	Fail    int64  `json:"fail"`
	Active  int32  `json:"active"`
}

// LogEntry is sent to the frontend console via WebSocket.
type LogEntry struct {
	Type    string `json:"type"`  // "log"
	Level   string `json:"level"` // info, success, error, warn
	Message string `json:"message"`
}

// WSMessage is the generic envelope for WebSocket communication.
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// NewStats initializes a Stats object with per-layer tracking.
func NewStats() *Stats {
	s := &Stats{
		StartTime: time.Now(),
	}
	layerNames := []string{"Chunked Abuse", "Recursive Params", "Cache Bypass",
		"Connection Pool", "Parser Stress"}
	for i, name := range layerNames {
		s.Layers[i] = &LayerStats{Name: name}
	}
	return s
}

// Reset resets all counters to zero.
func (s *Stats) Reset() {
	s.TotalRequests.Store(0)
	s.SuccessRequests.Store(0)
	s.FailedRequests.Store(0)
	s.ActiveWorkers.Store(0)
	s.BytesSent.Store(0)
	s.BytesRecv.Store(0)
	s.StartTime = time.Now()
	for _, l := range s.Layers {
		l.Requests.Store(0)
		l.Success.Store(0)
		l.Fail.Store(0)
		l.ActiveWorkers.Store(0)
	}
}

// Snapshot returns a point-in-time StatsSnapshot for the frontend.
func (s *Stats) Snapshot() StatsSnapshot {
	elapsed := time.Since(s.StartTime).Milliseconds()
	total := s.TotalRequests.Load()
	var rps int64
	if elapsed > 0 {
		rps = total * 1000 / elapsed
	}
	layers := make([]LayerSnapshot, 5)
	for i, l := range s.Layers {
		layers[i] = LayerSnapshot{
			Name:    l.Name,
			Req:     l.Requests.Load(),
			Success: l.Success.Load(),
			Fail:    l.Fail.Load(),
			Active:  l.ActiveWorkers.Load(),
		}
	}
	return StatsSnapshot{
		Type:    "stats",
		RPS:     rps,
		Total:   total,
		Success: s.SuccessRequests.Load(),
		Fail:    s.FailedRequests.Load(),
		Active:  s.ActiveWorkers.Load(),
		Uptime:  elapsed,
		Layers:  layers,
	}
}
