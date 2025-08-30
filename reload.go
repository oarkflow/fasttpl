package fasttpl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ----------------------------- Template Reload Manager -----------------------------

// ReloadCallback is called when a template file is reloaded
type ReloadCallback func(filename string, template *Template, err error)

// ReloadManager manages automatic template reloading
type ReloadManager struct {
	mu            sync.RWMutex
	watched       map[string]*watchInfo
	callbacks     []ReloadCallback
	stopChan      chan struct{}
	stopped       bool
	checkInterval time.Duration
}

type watchInfo struct {
	lastModTime time.Time
	template    *Template
	dependents  map[string]bool // files that depend on this template
}

// NewReloadManager creates a new reload manager
func NewReloadManager(checkInterval time.Duration) *ReloadManager {
	if checkInterval == 0 {
		checkInterval = 1 * time.Second
	}
	return &ReloadManager{
		watched:       make(map[string]*watchInfo),
		callbacks:     make([]ReloadCallback, 0),
		stopChan:      make(chan struct{}),
		checkInterval: checkInterval,
	}
}

// WatchFile adds a file to be watched for changes
func (rm *ReloadManager) WatchFile(filename string, template *Template) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("watching file %q: %w", filename, err)
	}

	rm.watched[filename] = &watchInfo{
		lastModTime: info.ModTime(),
		template:    template,
		dependents:  make(map[string]bool),
	}

	return nil
}

// WatchDirectory watches a directory for template files
func (rm *ReloadManager) WatchDirectory(dir string, opts ...Option) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".tpl") {
			filename := filepath.Join(dir, name)
			tmpl, err := CompileFile(filename, opts...)
			if err != nil {
				// Skip files that can't be compiled
				continue
			}

			err = rm.WatchFile(filename, tmpl)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// AddCallback adds a callback to be called when templates are reloaded
func (rm *ReloadManager) AddCallback(callback ReloadCallback) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.callbacks = append(rm.callbacks, callback)
}

// Start begins the file watching process
func (rm *ReloadManager) Start() {
	go rm.watchLoop()
}

// Stop stops the file watching process
func (rm *ReloadManager) Stop() {
	rm.mu.Lock()
	if !rm.stopped {
		rm.stopped = true
		close(rm.stopChan)
	}
	rm.mu.Unlock()
}

// GetTemplate returns the current template for a file, reloading if necessary
func (rm *ReloadManager) GetTemplate(filename string, opts ...Option) (*Template, error) {
	rm.mu.RLock()
	info, exists := rm.watched[filename]
	rm.mu.RUnlock()

	if !exists {
		// File not being watched, compile it
		tmpl, err := CompileFile(filename, opts...)
		if err != nil {
			return nil, err
		}
		return tmpl, nil
	}

	// Check if file has been modified
	stat, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("stat file %q: %w", filename, err)
	}

	if stat.ModTime().After(info.lastModTime) {
		// File has been modified, reload it
		tmpl, err := CompileFile(filename, opts...)
		if err != nil {
			return nil, fmt.Errorf("reloading template %q: %w", filename, err)
		}

		// Update the watch info
		rm.mu.Lock()
		info.lastModTime = stat.ModTime()
		info.template = tmpl
		rm.mu.Unlock()

		// Notify callbacks
		for _, callback := range rm.callbacks {
			callback(filename, tmpl, nil)
		}

		return tmpl, nil
	}

	return info.template, nil
}

// watchLoop runs the file watching loop
func (rm *ReloadManager) watchLoop() {
	ticker := time.NewTicker(rm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopChan:
			return
		case <-ticker.C:
			rm.checkFiles()
		}
	}
}

// checkFiles checks all watched files for modifications
func (rm *ReloadManager) checkFiles() {
	rm.mu.RLock()
	files := make([]string, 0, len(rm.watched))
	for filename := range rm.watched {
		files = append(files, filename)
	}
	rm.mu.RUnlock()

	for _, filename := range files {
		rm.checkFile(filename)
	}
}

// checkFile checks a single file for modifications
func (rm *ReloadManager) checkFile(filename string) {
	stat, err := os.Stat(filename)
	if err != nil {
		// File might have been deleted, skip for now
		return
	}

	rm.mu.RLock()
	info, exists := rm.watched[filename]
	rm.mu.RUnlock()

	if !exists {
		return
	}

	if stat.ModTime().After(info.lastModTime) {
		// File has been modified, reload it
		tmpl, err := CompileFile(filename)
		if err != nil {
			// Notify callbacks of the error
			for _, callback := range rm.callbacks {
				callback(filename, nil, err)
			}
			return
		}

		// Update the watch info
		rm.mu.Lock()
		info.lastModTime = stat.ModTime()
		info.template = tmpl
		rm.mu.Unlock()

		// Notify callbacks
		for _, callback := range rm.callbacks {
			callback(filename, tmpl, nil)
		}
	}
}
