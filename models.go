package main

import (
	"sync/atomic"
	"time"
)

type LayerConfig struct {
	L1 int `json:"l1"`
	L2 int `json:"l2"`
	L3 int `json:"l3"`
	L4 int `json:"l4"`
	L5 int `json:"l5"`
}

type AttackRequest struct {
	Target       string      `json:"target"`
	Layers       LayerConfig `json:"layers"`
	Duration     int         `json:"duration"`
	ProxyEnabled bool        `json:"proxy_enabled"`
}

type AttackResponse struct {
	AttackID string `json:"attack_id,omitempty"`
	Error    string `json:"error,omitempty"`
	Success  bool   `json:"success"`
}

type AttackStatus struct {
	Active   bool   `json:"active"`
	AttackID string `json:"attack_id"`
	Target   string `json:"target"`
	Uptime   int64  `json:"uptime_ms"`
}

type Stats struct {
	TotalRequests   atomic.Int64
	SuccessRequests atomic.Int64
	FailedRequests  atomic.Int64
	ActiveWorkers   atomic.Int32
	BytesSent       atomic.Int64
	BytesRecv       atomic.Int64
	StartTime       time.Time
	Layers          [5]*LayerStats
}

type LayerStats struct {
	Name          string
	Requests      atomic.Int64
	Success       atomic.Int64
	Fail          atomic.Int64
	ActiveWorkers atomic.Int32
}

type StatsSnapshot struct {
	Type    string          `json:"type"`
	RPS     int64           `json:"rps"`
	Total   int64           `json:"total"`
	Success int64           `json:"success"`
	Fail    int64           `json:"fail"`
	Active  int32           `json:"active"`
	Uptime  int64           `json:"uptime"`
	Layers  []LayerSnapshot `json:"layers,omitempty"`
}

type LayerSnapshot struct {
	Name    string `json:"name"`
	Req     int64  `json:"req"`
	Success int64  `json:"success"`
	Fail    int64  `json:"fail"`
	Active  int32  `json:"active"`
}

type LogEntry struct {
	Type    string `json:"type"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

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
