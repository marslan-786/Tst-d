package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
  "sync"
	"time"
)

// Server holds all dependencies for HTTP handlers.
type Server struct {
	cfg      *Config
	proxyMgr *ProxyManager
	hub      *WebSocketHub
	attacks  *AttackRegistry
	indexHTML []byte
}

// NewServer creates a new Server instance.
func NewServer(cfg *Config, pm *ProxyManager, hub *WebSocketHub, ar *AttackRegistry) *Server {
	return &Server{
		cfg:      cfg,
		proxyMgr: pm,
		hub:      hub,
		attacks:  ar,
	}
}

// SetIndexHTML sets the embedded index.html content.
func (s *Server) SetIndexHTML(html []byte) {
	s.indexHTML = html
}

// Router returns the configured http.Handler.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Frontend
	mux.HandleFunc("/", s.handleIndex)

	// API
	mux.HandleFunc("/api/attack/start", s.handleStart)
	mux.HandleFunc("/api/attack/stop", s.handleStop)
	mux.HandleFunc("/api/attack/status", s.handleStatus)

	// WebSocket
	mux.HandleFunc("/ws/console", s.handleWebSocket)

	// CORS middleware wrapper
	return corsMiddleware(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(s.indexHTML)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AttackResponse{Error: "method not allowed", Success: false})
		return
	}

	var req AttackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, AttackResponse{Error: "invalid JSON body", Success: false})
		return
	}

	if req.Target == "" {
		writeJSON(w, http.StatusBadRequest, AttackResponse{Error: "target URL is required", Success: false})
		return
	}

	// Validate target
	targetURL, err := ParseTarget(req.Target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, AttackResponse{Error: err.Error(), Success: false})
		return
	}

	// Apply default layer configs if zero
	layers := req.Layers
	if layers.L1 == 0 { layers.L1 = s.cfg.DefaultLayers.L1 }
	if layers.L2 == 0 { layers.L2 = s.cfg.DefaultLayers.L2 }
	if layers.L3 == 0 { layers.L3 = s.cfg.DefaultLayers.L3 }
	if layers.L4 == 0 { layers.L4 = s.cfg.DefaultLayers.L4 }
	if layers.L5 == 0 { layers.L5 = s.cfg.DefaultLayers.L5 }

	duration := time.Duration(req.Duration) * time.Second
	if duration <= 0 {
		duration = s.cfg.DefaultDuration
	}

	// Create and start orchestrator
	orch := NewOrchestrator(targetURL, layers, duration, s.proxyMgr, s.hub, s.cfg)
	s.attacks.Add(orch)

	go func() {
		orch.Start()
		s.attacks.Remove(orch.ID())
		// Notify frontend that attack stopped
		s.hub.BroadcastLog("info", "Attack finished: "+orch.ID())
	}()

	log.Printf("[api] Attack started: %s -> %s (duration: %v)", orch.ID(), targetURL.String(), duration)
	writeJSON(w, http.StatusOK, AttackResponse{AttackID: orch.ID(), Success: true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AttackResponse{Error: "method not allowed", Success: false})
		return
	}

	var req struct {
		AttackID string `json:"attack_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, AttackResponse{Error: "invalid JSON body", Success: false})
		return
	}

	if req.AttackID == "" {
		writeJSON(w, http.StatusBadRequest, AttackResponse{Error: "attack_id is required", Success: false})
		return
	}

	orch := s.attacks.Get(req.AttackID)
	if orch == nil {
		writeJSON(w, http.StatusNotFound, AttackResponse{Error: "attack not found", Success: false})
		return
	}

	orch.Stop()
	log.Printf("[api] Attack stopped: %s", req.AttackID)
	writeJSON(w, http.StatusOK, AttackResponse{AttackID: req.AttackID, Success: true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, AttackResponse{Error: "method not allowed", Success: false})
		return
	}

	attackID := r.URL.Query().Get("attack_id")
	if attackID == "" {
		writeJSON(w, http.StatusBadRequest, AttackResponse{Error: "attack_id query param required", Success: false})
		return
	}

	orch := s.attacks.Get(attackID)
	if orch == nil {
		writeJSON(w, http.StatusOK, AttackStatus{Active: false})
		return
	}

	writeJSON(w, http.StatusOK, AttackStatus{
		Active:   true,
		AttackID: orch.ID(),
		Target:   orch.Target(),
		Uptime:   orch.UptimeMs(),
	})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	attackID := r.URL.Query().Get("attack_id")
	if attackID == "" {
		http.Error(w, "attack_id query param required", http.StatusBadRequest)
		return
	}

	orch := s.attacks.Get(attackID)
	if orch == nil {
		http.Error(w, "attack not found or already finished", http.StatusNotFound)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] Upgrade error: %v", err)
		return
	}

	client := NewClient(s.hub, conn, orch)
	s.hub.Register(client)
	go client.WritePump()
	go client.ReadPump()

	log.Printf("[ws] Client connected for attack %s", attackID)
}

// corsMiddleware adds CORS headers for development.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// AttackRegistry holds all active attacks (thread-safe).
type AttackRegistry struct {
	attacks map[string]*Orchestrator
	mu      sync.RWMutex
}

func NewAttackRegistry() *AttackRegistry {
	return &AttackRegistry{attacks: make(map[string]*Orchestrator)}
}

func (ar *AttackRegistry) Add(o *Orchestrator) {
	ar.mu.Lock()
	ar.attacks[o.ID()] = o
	ar.mu.Unlock()
}

func (ar *AttackRegistry) Remove(id string) {
	ar.mu.Lock()
	delete(ar.attacks, id)
	ar.mu.Unlock()
}

func (ar *AttackRegistry) Get(id string) *Orchestrator {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	return ar.attacks[id]
}

func (ar *AttackRegistry) StopAll() {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	for _, o := range ar.attacks {
		o.Stop()
	}
}
