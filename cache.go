package fasttpl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

// WarmupCache pre-allocates common buffer sizes to warm up pools
func WarmupCache() {
	// Pre-allocate some buffers of common sizes
	for i := 0; i < 10; i++ {
		buf := bufPool.Get().(*bytes.Buffer)
		buf.Grow(1024) // Common size
		bufPool.Put(buf)

		sb := stringBuilderPool.Get().(*strings.Builder)
		sb.Grow(512)
		stringBuilderPool.Put(sb)

		ctx := renderCtxPool.Get().(*renderCtx)
		renderCtxPool.Put(ctx)
	}
}

// ----------------------------- Template compilation cache ----------------

type CompileCache struct {
	mu        sync.RWMutex
	templates map[string]*Template
	maxSize   int
}

var globalCompileCache = &CompileCache{
	templates: make(map[string]*Template),
	maxSize:   500,
}

// ----------------------------- Field reflection cache -----------------------

type fieldCache struct {
	mu    sync.RWMutex
	cache map[fieldCacheKey]*fieldInfo
}

type fieldCacheKey struct {
	typ  reflect.Type
	name string
}

type fieldInfo struct {
	index    []int
	found    bool
	isMethod bool
}

func newFieldCache() *fieldCache {
	return &fieldCache{
		cache: make(map[fieldCacheKey]*fieldInfo),
	}
}

type valueCache struct {
	mu    sync.RWMutex
	cache map[string]reflect.Value
}

var globalValueCache = &valueCache{
	cache: make(map[string]reflect.Value),
}

func (vc *valueCache) get(s string) reflect.Value {
	vc.mu.RLock()
	v, ok := vc.cache[s]
	vc.mu.RUnlock()
	if ok {
		return v
	}
	v = reflect.ValueOf(s)
	vc.mu.Lock()
	vc.cache[s] = v
	vc.mu.Unlock()
	return v
}

// CompileCached compiles a template with in-memory caching
func CompileCached(src string, opts ...Option) (*Template, error) {
	return globalCompileCache.Compile(src, opts...)
}

func (cc *CompileCache) Compile(src string, opts ...Option) (*Template, error) {
	// Create a cache key from source and options
	key := src // Simple key - could hash for very large templates

	cc.mu.RLock()
	tmpl, exists := cc.templates[key]
	cc.mu.RUnlock()

	if exists {
		return tmpl, nil
	}

	// Compile new template
	tmpl, err := Compile(src, opts...)
	if err != nil {
		return nil, err
	}

	// Cache result
	cc.mu.Lock()
	if len(cc.templates) >= cc.maxSize {
		// Simple eviction: remove first entry
		for k := range cc.templates {
			delete(cc.templates, k)
			break
		}
	}
	cc.templates[key] = tmpl
	cc.mu.Unlock()

	return tmpl, nil
}

type compileOptions struct {
	filters    Filters
	leftDelim  string
	rightDelim string
}

// FileCache provides template file caching with modification time checking
type FileCache struct {
	mu        sync.RWMutex
	templates map[string]*cachedTemplate
	maxSize   int
}

type cachedTemplate struct {
	template *Template
	modTime  time.Time
}

// Global file cache instance
var globalFileCache = &FileCache{
	templates: make(map[string]*cachedTemplate),
	maxSize:   1000, // configurable
}

// NewFileCache creates a new file cache with specified max size
func NewFileCache(maxSize int) *FileCache {
	return &FileCache{
		templates: make(map[string]*cachedTemplate),
		maxSize:   maxSize,
	}
}

type Option func(*compileOptions)

// CompileFile compiles a template from file with caching and automatic include discovery
func CompileFile(filename string, opts ...Option) (*Template, error) {
	return globalFileCache.CompileFile(filename, opts...)
}

// CompileFile compiles a template from file with caching and automatic include discovery
func (fc *FileCache) CompileFile(filename string, opts ...Option) (*Template, error) {
	// Get file info first
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("template file %q: %w", filename, err)
	}

	// Check cache
	fc.mu.RLock()
	cached, exists := fc.templates[filename]
	fc.mu.RUnlock()

	if exists && !cached.modTime.Before(info.ModTime()) && len(opts) == 0 {
		return cached.template, nil
	}

	// Read and compile
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading template %q: %w", filename, err)
	}

	tmpl, err := Compile(string(content), opts...)
	if err != nil {
		return nil, fmt.Errorf("compiling template %q: %w", filename, err)
	}

	// Auto-discover and register partials in the same directory
	dir := filepath.Dir(filename)
	base := filepath.Base(filename)
	baseNoExt := strings.TrimSuffix(base, filepath.Ext(base))

	// Look for partial files (e.g., _header.html, _footer.html)
	entries, err := os.ReadDir(dir)
	if err == nil { // Don't fail if we can't read directory
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || name == base {
				continue
			}

			// Register files that start with underscore as partials
			if strings.HasPrefix(name, "_") {
				partialPath := filepath.Join(dir, name)
				partialName := strings.TrimPrefix(name, "_")
				partialName = strings.TrimSuffix(partialName, filepath.Ext(partialName))

				// Skip if partial name matches the main template's base name (to avoid conflicts)
				if partialName == baseNoExt {
					continue
				}

				// Compile partial without include discovery to avoid infinite recursion
				partialContent, err := os.ReadFile(partialPath)
				if err != nil {
					// Skip failed partials but don't fail the main compilation
					continue
				}

				partial, err := Compile(string(partialContent), opts...)
				if err != nil {
					// Skip failed partials but don't fail the main compilation
					continue
				}
				tmpl.RegisterPartial(partialName, partial)
			}
		}
	}

	// Cache the result only if no opts
	if len(opts) == 0 {
		fc.mu.Lock()
		if len(fc.templates) >= fc.maxSize {
			// Simple LRU: remove first entry (could be improved with proper LRU)
			for k := range fc.templates {
				delete(fc.templates, k)
				break
			}
		}
		fc.templates[filename] = &cachedTemplate{
			template: tmpl,
			modTime:  info.ModTime(),
		}
		fc.mu.Unlock()
	}

	return tmpl, nil
}

// ClearCache clears the file cache
func (fc *FileCache) ClearCache() {
	fc.mu.Lock()
	fc.templates = make(map[string]*cachedTemplate)
	fc.mu.Unlock()
}
