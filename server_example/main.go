package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/oarkflow/fasttpl"
)

type Server struct {
	engine *fasttpl.Engine
	mu     sync.RWMutex
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) SetEngine(engine *fasttpl.Engine) {
	s.mu.Lock()
	s.engine = engine
	s.mu.Unlock()
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path
	if page == "/" {
		page = "/index"
	}

	s.mu.RLock()
	engine := s.engine
	s.mu.RUnlock()

	if engine == nil {
		http.Error(w, "Template engine not initialized", 500)
		return
	}

	// Prepare data
	data := map[string]any{
		"title": "FastTpl Auto-Reload Demo",
		"site": map[string]any{
			"name": "FastTpl Demo",
		},
		"currentTime": time.Now().Format("2006-01-02 15:04:05"),
		"year":        time.Now().Year(),
		"lastUpdate":  time.Now().Format("2006-01-02 15:04:05"),
		"user": map[string]any{
			"name":     "Admin User",
			"loggedIn": true,
		},
		"posts": []map[string]any{
			{
				"title":  "Getting Started with FastTpl",
				"author": "FastTpl Team",
				"date":   "2025-01-15",
			},
			{
				"title":  "Template Auto-Reload Feature",
				"author": "FastTpl Team",
				"date":   "2025-01-20",
			},
			{
				"title":  "Performance Optimization Tips",
				"author": "FastTpl Team",
				"date":   "2025-01-25",
			},
		},
	}

	// Render the template using the engine (it will use the default layout)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templateName := page[1:] // Remove leading slash
	if err := engine.Render(w, templateName, data); err != nil {
		// Try to serve the index page as fallback
		if templateName != "index" {
			if err := engine.Render(w, "index", data); err != nil {
				http.Error(w, fmt.Sprintf("Template error: %v", err), 500)
				return
			}
		} else {
			http.Error(w, fmt.Sprintf("Template error: %v", err), 500)
			return
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{
		"status": "running",
		"engine_loaded": true,
		"default_layout": "layout",
		"message": "FastTpl Engine with layout support is running!"
	}`))
}

func RunReloadServer() {
	server := NewServer()
	engine, err := fasttpl.NewTemplate("templates", ".html",
		fasttpl.WithLayout("layout"),
		fasttpl.WithReloadInterval(500*time.Millisecond))
	if err != nil {
		panic(err)
	}
	server.SetEngine(engine)

	// Set up HTTP routes
	http.HandleFunc("/", server.handlePage)
	http.HandleFunc("/status", server.handleStatus)

	fmt.Println("🚀 FastTpl Engine Server starting...")
	fmt.Println("📁 Templates directory: ./templates/")
	fmt.Println("🔄 Engine-based rendering with layout support and auto-reload")
	fmt.Println("🌐 Server running at: http://localhost:8080")
	fmt.Println("📊 Status endpoint: http://localhost:8080/status")
	fmt.Println("")
	fmt.Println("Try editing files like:")
	fmt.Println("  - templates/index.html")
	fmt.Println("  - templates/_header.html")
	fmt.Println("  - templates/_footer.html")
	fmt.Println("")
	fmt.Println("Changes will be automatically reloaded!")

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nShutting down server...")
		engine.Stop()
		os.Exit(0)
	}()

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func main() {
	RunReloadServer()
}
