package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oarkflow/fasttpl"
)

type Server struct {
	reloadManager *fasttpl.ReloadManager
	templates     map[string]*fasttpl.Template
	mu            sync.RWMutex
}

func NewServer() *Server {
	rm := fasttpl.NewReloadManager(500 * time.Millisecond) // Check every 500ms

	// Add reload callback to log when templates are reloaded
	rm.AddCallback(func(filename string, template *fasttpl.Template, err error) {
		if err != nil {
			log.Printf("Error reloading template %s: %v", filename, err)
		} else {
			log.Printf("Template reloaded: %s", filename)
		}
	})

	return &Server{
		reloadManager: rm,
		templates:     make(map[string]*fasttpl.Template),
	}
}

func (s *Server) loadTemplates() error {
	// Create templates directory if it doesn't exist
	templatesDir := "templates"
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return fmt.Errorf("creating templates directory: %w", err)
	}

	// Create sample templates
	templates := map[string]string{
		"layout.html": `
<!DOCTYPE html>
<html>
<head>
    <title>{{ title }}</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .header { background: #f0f0f0; padding: 20px; border-radius: 5px; }
        .content { margin: 20px 0; }
        .footer { border-top: 1px solid #ccc; padding-top: 20px; color: #666; }
        .reload-notice { background: #e8f5e8; border: 1px solid #4caf50; color: #2e7d32; padding: 10px; border-radius: 3px; margin: 10px 0; }
    </style>
</head>
<body>
    {{ include "header" }}
    <div class="content">
        {{ include "content" }}
    </div>
    {{ include "footer" }}
</body>
</html>`,
		"_header.html": `
<div class="header">
    <h1>{{ site.name }}</h1>
    <nav>
        <a href="/">Home</a> |
        <a href="/about">About</a> |
        <a href="/contact">Contact</a>
    </nav>
</div>`,
		"_footer.html": `
<div class="footer">
    <p>&copy; {{ year }} {{ site.name }}. All rights reserved.</p>
    <p>Last updated: {{ lastUpdate }}</p>
</div>`,
		"index.html": `
<div class="reload-notice">
    <strong>Template Auto-Reload Demo</strong><br>
    Edit the template files in the 'templates/' directory and refresh this page to see changes instantly!
</div>

<h2>Welcome to {{ site.name }}</h2>
<p>This is the home page. Current time: {{ currentTime }}</p>

<h3>Features Demonstrated:</h3>
<ul>
    <li>Automatic template reloading when files change</li>
    <li>Template includes with partials</li>
    <li>FastTpl's high-performance rendering</li>
    <li>Live development experience</li>
</ul>

{{ if user.loggedIn }}
<div style="background: #e3f2fd; padding: 15px; border-radius: 5px; margin: 20px 0;">
    <h3>Hello, {{ user.name }}!</h3>
    <p>You are logged in as an administrator.</p>
</div>
{{ else }}
<div style="background: #fff3e0; padding: 15px; border-radius: 5px; margin: 20px 0;">
    <p><a href="/login">Login</a> to access admin features.</p>
</div>
{{ end }}

<h3>Recent Posts</h3>
<ul>
{{ range post in posts }}
    <li>
        <strong>{{ $post.title }}</strong> by {{ $post.author }}<br>
        <small>{{ $post.date }}</small>
    </li>
{{ end }}
</ul>`,
		"about.html": `
<h2>About {{ site.name }}</h2>
<p>This is a demonstration of FastTpl's automatic template reloading feature.</p>

<h3>How it works:</h3>
<ol>
    <li>The server watches template files for changes</li>
    <li>When a file is modified, it's automatically recompiled</li>
    <li>The next request uses the updated template</li>
    <li>No server restart required!</li>
</ol>

<h3>Try it:</h3>
<p>Edit any template file in the <code>templates/</code> directory and refresh this page.</p>

<h3>Performance:</h3>
<p>FastTpl provides high-performance template rendering with:</p>
<ul>
    <li>Zero-allocation rendering paths</li>
    <li>Compiled templates for maximum speed</li>
    <li>Cached reflection and field access</li>
    <li>Object pooling for optimal memory usage</li>
</ul>`,
	}

	// Write template files
	for filename, content := range templates {
		filepath := filepath.Join(templatesDir, filename)
		if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing template %s: %w", filename, err)
		}
	}

	// Watch the templates directory
	if err := s.reloadManager.WatchDirectory(templatesDir); err != nil {
		return fmt.Errorf("watching templates directory: %w", err)
	}

	log.Println("Templates loaded and directory is being watched for changes")
	return nil
}

func (s *Server) getTemplate(name string) (*fasttpl.Template, error) {
	filename := filepath.Join("templates", name+".html")

	// Try to get from reload manager (will reload if necessary)
	tmpl, err := s.reloadManager.GetTemplate(filename)
	if err != nil {
		// Fallback to regular compilation if not being watched
		tmpl, err = fasttpl.CompileFile(filename)
		if err != nil {
			return nil, err
		}
	}

	return tmpl, nil
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path
	if page == "/" {
		page = "/index"
	}

	// Get the content template
	contentTmpl, err := s.getTemplate(page[1:]) // Remove leading slash
	if err != nil {
		// Try to serve the index page as fallback
		if page != "/index" {
			contentTmpl, err = s.getTemplate("index")
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Template error: %v", err), 500)
			return
		}
	}

	// Get the layout template
	layoutTmpl, err := s.getTemplate("layout")
	if err != nil {
		http.Error(w, fmt.Sprintf("Layout template error: %v", err), 500)
		return
	}

	// Register the content as a partial in the layout
	layoutTmpl.RegisterPartial("content", contentTmpl)

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

	// Render the template
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := layoutTmpl.Render(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Render error: %v", err), 500)
		return
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{
		"status": "running",
		"templates_watched": true,
		"reload_interval": "500ms",
		"message": "Edit template files and refresh pages to see auto-reload in action!"
	}`))
}

func RunReloadServer() {
	server := NewServer()

	// Load initial templates
	if err := server.loadTemplates(); err != nil {
		log.Fatal("Failed to load templates:", err)
	}

	// Start the reload manager
	server.reloadManager.Start()
	defer server.reloadManager.Stop()

	// Set up HTTP routes
	http.HandleFunc("/", server.handlePage)
	http.HandleFunc("/status", server.handleStatus)

	// Serve static files from templates directory
	http.Handle("/templates/", http.StripPrefix("/templates/", http.FileServer(http.Dir("templates"))))

	fmt.Println("🚀 FastTpl Auto-Reload Server starting...")
	fmt.Println("📁 Templates directory: ./templates/")
	fmt.Println("🔄 Auto-reload enabled - edit template files and refresh browser")
	fmt.Println("🌐 Server running at: http://localhost:8080")
	fmt.Println("📊 Status endpoint: http://localhost:8080/status")
	fmt.Println("")
	fmt.Println("Try editing files like:")
	fmt.Println("  - templates/index.html")
	fmt.Println("  - templates/_header.html")
	fmt.Println("  - templates/_footer.html")
	fmt.Println("")
	fmt.Println("Then refresh http://localhost:8080 to see changes instantly!")

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func main() {
	RunReloadServer()
}
