package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	cfg       *Config
	proxyMgr  *ProxyManager
	hub       *WebSocketHub
	attacks   *AttackRegistry
	indexHTML []byte
}

func NewServer(cfg *Config, pm *ProxyManager, hub *WebSocketHub, ar *AttackRegistry) *Server {
	return &Server{
		cfg:      cfg,
		proxyMgr: pm,
		hub:      hub,
		attacks:  ar,
	}
}

func (s *Server) SetIndexHTML(html []byte) {
	s.indexHTML = html
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/attack/create", s.handleCreate)
	mux.HandleFunc("/api/attack/start", s.handleStart)
	mux.HandleFunc("/api/attack/stop", s.handleStop)
	mux.HandleFunc("/api/attack/status", s.handleStatus)
	mux.HandleFunc("/api/attack/list", s.handleList)
	mux.HandleFunc("/api/attack/delete", s.handleDelete)
	mux.HandleFunc("/api/attack/redeploy", s.handleRedeploy)
	mux.HandleFunc("/ws/console", s.handleWebSocket)

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

// ==================== CREATE ====================
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	var req CreateAttackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body", "success": false})
		return
	}

	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "target URL is required", "success": false})
		return
	}

	// Validate and parse target
	targetURL, err := ParseTarget(req.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error(), "success": false})
		return
	}

	// Generate smart paths
	smartPaths := DetectSmartPaths(targetURL.String())

	// Create attack info
	attackID := generateID()
	attackInfo := &AttackInfo{
		ID:            attackID,
		URL:           targetURL.String(),
		TargetPaths:   smartPaths,
		CreatedAt:     time.Now(),
		Status:        "stopped",
		LastResponses: make([]ResponseEntry, 0, 5),
	}

	s.attacks.Store(attackID, attackInfo)
	log.Printf("[api] Attack created: %s -> %s", attackID, targetURL.String())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"attack_id": attackID,
		"url":       targetURL.String(),
		"paths":     smartPaths,
	})
}

// ==================== START ====================
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	var req struct {
		AttackID     string      `json:"attack_id"`
		Layers       LayerConfig `json:"layers,omitempty"`
		Duration     int         `json:"duration,omitempty"`
		ProxyEnabled bool        `json:"proxy_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body", "success": false})
		return
	}

	if req.AttackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "attack_id is required", "success": false})
		return
	}

	info := s.attacks.Load(req.AttackID)
	if info == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "attack not found", "success": false})
		return
	}

	if info.Status == "active" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "attack already running", "success": false})
		return
	}

	// Parse target
	targetURL, err := ParseTarget(info.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid stored URL: " + err.Error(), "success": false})
		return
	}

	// Apply default layers if zero
	layers := req.Layers
	if layers.L1 == 0 {
		layers.L1 = s.cfg.DefaultLayers.L1
	}
	if layers.L2 == 0 {
		layers.L2 = s.cfg.DefaultLayers.L2
	}
	if layers.L3 == 0 {
		layers.L3 = s.cfg.DefaultLayers.L3
	}
	if layers.L4 == 0 {
		layers.L4 = s.cfg.DefaultLayers.L4
	}
	if layers.L5 == 0 {
		layers.L5 = s.cfg.DefaultLayers.L5
	}

	workers := LayerWorkers{
		L1: layers.L1,
		L2: layers.L2,
		L3: layers.L3,
		L4: layers.L4,
		L5: layers.L5,
	}

	duration := time.Duration(req.Duration) * time.Second
	if duration <= 0 {
		duration = s.cfg.DefaultDuration
	}

	// Create orchestrator with smart paths
	orch := NewOrchestrator(targetURL, workers, duration, s.proxyMgr, s.hub, s.cfg, req.ProxyEnabled, info.TargetPaths)
	info.Orchestrator = orch
	info.Status = "active"
	s.attacks.Store(req.AttackID, info)

	go func() {
		orch.Start()
		info := s.attacks.Load(req.AttackID)
		if info != nil {
			info.Status = "stopped"
			info.Orchestrator = nil
			s.attacks.Store(req.AttackID, info)
		}
		s.hub.BroadcastLog("info", "Attack finished: "+req.AttackID)
	}()

	log.Printf("[api] Attack started: %s -> %s (workers: %d)", req.AttackID, targetURL.String(), workers.Total())
	writeJSON(w, http.StatusOK, AttackResponse{AttackID: req.AttackID, Success: true})
}

// ==================== STOP ====================
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	var req struct {
		AttackID string `json:"attack_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body", "success": false})
		return
	}

	if req.AttackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "attack_id is required", "success": false})
		return
	}

	info := s.attacks.Load(req.AttackID)
	if info == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "attack not found", "success": false})
		return
	}

	if info.Orchestrator != nil {
		info.Orchestrator.Stop()
	}
	info.Status = "stopped"
	s.attacks.Store(req.AttackID, info)

	log.Printf("[api] Attack stopped: %s", req.AttackID)
	writeJSON(w, http.StatusOK, AttackResponse{AttackID: req.AttackID, Success: true})
}

// ==================== STATUS ====================
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	attackID := r.URL.Query().Get("attack_id")
	if attackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "attack_id query param required", "success": false})
		return
	}

	info := s.attacks.Load(attackID)
	if info == nil {
		writeJSON(w, http.StatusOK, AttackStatus{Active: false})
		return
	}

	uptime := int64(0)
	if info.Orchestrator != nil {
		uptime = info.Orchestrator.UptimeMs()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":         info.Status == "active",
		"attack_id":      info.ID,
		"target":         info.URL,
		"uptime_ms":      uptime,
		"status":         info.Status,
		"created_at":     info.CreatedAt,
		"total_redeploys": info.TotalRedeploys,
		"last_responses": info.LastResponses,
	})
}

// ==================== LIST ====================
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	attacks := s.attacks.List()
	attackList := make([]map[string]interface{}, 0, len(attacks))
	for _, info := range attacks {
		// Extract end part of URL for display
		displayURL := info.URL
		if idx := strings.LastIndex(info.URL, "/"); idx > 10 {
			displayURL = "..." + info.URL[idx:]
		}

		attackList = append(attackList, map[string]interface{}{
			"id":            info.ID,
			"url":           info.URL,
			"display_url":   displayURL,
			"status":        info.Status,
			"created_at":    info.CreatedAt,
			"total_redeploys": info.TotalRedeploys,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"attacks": attackList,
		"total":   len(attackList),
	})
}

// ==================== DELETE ====================
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	var req struct {
		AttackID string `json:"attack_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body", "success": false})
		return
	}

	if req.AttackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "attack_id is required", "success": false})
		return
	}

	info := s.attacks.Load(req.AttackID)
	if info != nil && info.Orchestrator != nil {
		info.Orchestrator.Stop()
	}

	s.attacks.Delete(req.AttackID)
	log.Printf("[api] Attack deleted: %s", req.AttackID)
	writeJSON(w, http.StatusOK, AttackResponse{AttackID: req.AttackID, Success: true})
}

// ==================== REDEPLOY ====================
func (s *Server) handleRedeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"error": "method not allowed", "success": false})
		return
	}

	var req struct {
		AttackID string `json:"attack_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid JSON body", "success": false})
		return
	}

	if req.AttackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "attack_id is required", "success": false})
		return
	}

	info := s.attacks.Load(req.AttackID)
	if info == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "attack not found", "success": false})
		return
	}

	// Stop current attack
	if info.Orchestrator != nil {
		info.Orchestrator.Stop()
		info.Orchestrator = nil
		time.Sleep(2 * time.Second)
	}

	info.Status = "deploying"
	info.TotalRedeploys++
	s.attacks.Store(req.AttackID, info)

	log.Printf("[api] Redeploy requested for attack: %s (redeploy #%d)", req.AttackID, info.TotalRedeploys)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"attack_id":      req.AttackID,
		"redeploy_count": info.TotalRedeploys,
	})
}

// ==================== WEBSOCKET ====================
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	attackID := r.URL.Query().Get("attack_id")
	if attackID == "" {
		http.Error(w, "attack_id query param required", http.StatusBadRequest)
		return
	}

	info := s.attacks.Load(attackID)
	if info == nil || info.Orchestrator == nil {
		http.Error(w, "attack not found or not running", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] Upgrade error: %v", err)
		return
	}

	client := NewClient(s.hub, conn, info.Orchestrator)
	s.hub.Register(client)
	go client.WritePump()
	go client.ReadPump()

	log.Printf("[ws] Client connected for attack %s", attackID)
}

// ==================== MIDDLEWARE ====================
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

// ==================== ATTACK REGISTRY ====================
type AttackRegistry struct {
	attacks map[string]*AttackInfo
	mu      sync.RWMutex
}

func NewAttackRegistry() *AttackRegistry {
	return &AttackRegistry{attacks: make(map[string]*AttackInfo)}
}

func (ar *AttackRegistry) Store(id string, info *AttackInfo) {
	ar.mu.Lock()
	ar.attacks[id] = info
	ar.mu.Unlock()
}

func (ar *AttackRegistry) Load(id string) *AttackInfo {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	return ar.attacks[id]
}

func (ar *AttackRegistry) Delete(id string) {
	ar.mu.Lock()
	delete(ar.attacks, id)
	ar.mu.Unlock()
}

func (ar *AttackRegistry) List() []*AttackInfo {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	list := make([]*AttackInfo, 0, len(ar.attacks))
	for _, v := range ar.attacks {
		list = append(list, v)
	}
	return list
}

func (ar *AttackRegistry) StopAll() {
	ar.mu.RLock()
	defer ar.mu.RUnlock()
	for _, info := range ar.attacks {
		if info.Orchestrator != nil {
			info.Orchestrator.Stop()
		}
	}
}
