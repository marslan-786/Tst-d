package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("🚀 Silent Stress Engine starting...")

	cfg := DefaultConfig()

	// Initialize proxy manager
	proxyMgr, err := NewProxyManager(cfg.Proxy)
	if err != nil {
		log.Fatalf("Failed to initialize proxy manager: %v", err)
	}

	// Create shared state
	hub := NewWebSocketHub()
	go hub.Run()

	attacks := NewAttackRegistry()

	// Create server
	srv := NewServer(cfg, proxyMgr, hub, attacks)

	// Extract the index.html from embedded FS
	indexHTML, err := fs.ReadFile(webFS, "web/index.html")
	if err != nil {
		log.Fatalf("Failed to read embedded index.html: %v", err)
	}
	srv.SetIndexHTML(indexHTML)

	httpServer := &http.Server{
		Addr:         "0.0.0.0:8080",
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Override port from config
	httpServer.Addr = "0.0.0.0:" + intToStr(cfg.Port)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		// Stop all running attacks
		attacks.StopAll()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Server listening on %s", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped.")
}

func intToStr(i int) string {
	if i == 0 {
		return "8080"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
