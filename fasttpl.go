package fasttpl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Filters map[string]func(string, []string) (string, error)

// Global reload manager instance
var globalReloadManager = NewReloadManager(1 * time.Second)

// WatchFile adds a file to the global reload manager
func WatchFile(filename string, template *Template) error {
	return globalReloadManager.WatchFile(filename, template)
}

// WatchDirectory adds a directory to the global reload manager
func WatchDirectory(dir string, opts ...Option) error {
	return globalReloadManager.WatchDirectory(dir, opts...)
}

// AddReloadCallback adds a callback to the global reload manager
func AddReloadCallback(callback ReloadCallback) {
	globalReloadManager.AddCallback(callback)
}

// StartReloader starts the global reload manager
func StartReloader() {
	globalReloadManager.Start()
}

// StopReloader stops the global reload manager
func StopReloader() {
	globalReloadManager.Stop()
}

// GetWatchedTemplate returns a template from the global reload manager
func GetWatchedTemplate(filename string, opts ...Option) (*Template, error) {
	return globalReloadManager.GetTemplate(filename, opts...)
}

// Compile parses and compiles a template string into a high-performance renderer.
func Compile(src string, opts ...Option) (*Template, error) {
	co := compileOptions{
		filters:    DefaultFilters(),
		leftDelim:  "{{",
		rightDelim: "}}",
	}
	for _, o := range opts {
		o(&co)
	}
	p := parser{
		src:        src,
		leftDelim:  co.leftDelim,
		rightDelim: co.rightDelim,
	}
	nodes, err := p.parse()
	if err != nil {
		return nil, err
	}
	root := sequence(nodes)
	return &Template{
		root:       root,
		parts:      make(map[string]*Template),
		filt:       co.filters,
		fieldCache: newFieldCache(),
	}, nil
}

// WithFilters allows registering/overriding filters.
func WithFilters(f Filters) Option { return func(co *compileOptions) { co.filters = f } }

// WithDelims allows setting custom delimiters.
func WithDelims(left, right string) Option {
	return func(co *compileOptions) {
		co.leftDelim = left
		co.rightDelim = right
	}
}

// ----------------------------- Template Engine -----------------------------

type EngineOptions struct {
	defaultLayout  string
	reloadInterval time.Duration
}

type EngineOption func(*EngineOptions)

func WithLayout(layout string) EngineOption {
	return func(eo *EngineOptions) { eo.defaultLayout = layout }
}

func WithReloadInterval(interval time.Duration) EngineOption {
	return func(eo *EngineOptions) { eo.reloadInterval = interval }
}

type Engine struct {
	templates     map[string]*Template
	defaultLayout string
	reloadManager *ReloadManager
	dir           string
	ext           string
	mu            sync.RWMutex
}

// Load loads all templates from the directory
func (e *Engine) Load() error {
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		return fmt.Errorf("reading directory %q: %w", e.dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), e.ext) {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), e.ext)
		path := filepath.Join(e.dir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading template %q: %w", path, err)
		}

		tmpl, err := Compile(string(content))
		if err != nil {
			return fmt.Errorf("compiling template %q: %w", path, err)
		}

		// Auto-discover and register partials in the same directory
		base := entry.Name()
		baseNoExt := strings.TrimSuffix(base, e.ext)

		// Look for partial files (e.g., _header.html, _footer.html)
		partialEntries, err := os.ReadDir(e.dir)
		if err == nil { // Don't fail if we can't read directory
			for _, partialEntry := range partialEntries {
				partialName := partialEntry.Name()
				if partialEntry.IsDir() || partialName == base {
					continue
				}

				// Register files that start with underscore as partials
				if strings.HasPrefix(partialName, "_") {
					partialPath := filepath.Join(e.dir, partialName)
					partialBaseName := strings.TrimPrefix(partialName, "_")
					partialBaseName = strings.TrimSuffix(partialBaseName, e.ext)

					// Skip if partial name matches the main template's base name (to avoid conflicts)
					if partialBaseName == baseNoExt {
						continue
					}

					// Compile partial without include discovery to avoid infinite recursion
					partialContent, err := os.ReadFile(partialPath)
					if err != nil {
						// Skip failed partials but don't fail the main compilation
						continue
					}

					partial, err := Compile(string(partialContent))
					if err != nil {
						// Skip failed partials but don't fail the main compilation
						continue
					}
					tmpl.RegisterPartial(partialBaseName, partial)
				}
			}
		}

		e.templates[name] = tmpl
	}

	return nil
}

// Stop stops the template reloading
func (e *Engine) Stop() {
	if e.reloadManager != nil {
		e.reloadManager.Stop()
	}
}

// Render renders the specified template with optional layout
func (e *Engine) Render(w io.Writer, tmplName string, data any, layout ...string) error {
	e.mu.RLock()
	tmpl, ok := e.templates[tmplName]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("template %q not found", tmplName)
	}

	var layoutTmpl *Template
	var layoutName string

	if len(layout) > 0 {
		layoutName = layout[0]
	} else {
		layoutName = e.defaultLayout
	}

	if layoutName != "" {
		e.mu.RLock()
		layoutTmpl, ok = e.templates[layoutName]
		e.mu.RUnlock()

		if !ok {
			return fmt.Errorf("layout template %q not found", layoutName)
		}
	}

	if layoutTmpl != nil {
		// Clone the layout to avoid modifying the original
		layoutCopy := &Template{
			root:       layoutTmpl.root,
			parts:      make(map[string]*Template),
			filt:       layoutTmpl.filt,
			fieldCache: layoutTmpl.fieldCache,
		}
		for k, v := range layoutTmpl.parts {
			layoutCopy.parts[k] = v
		}
		layoutCopy.RegisterPartial("content", tmpl)
		return layoutCopy.Render(w, data)
	} else {
		return tmpl.Render(w, data)
	}
}

// RenderString renders the specified template with optional layout and returns a string
func (e *Engine) RenderString(tmplName string, data any, layout ...string) (string, error) {
	sb := stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	defer stringBuilderPool.Put(sb)
	if err := e.Render(sb, tmplName, data, layout...); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func sequence(nodes []node) node {
	if len(nodes) == 1 {
		return nodes[0]
	}
	return seqNode(nodes)
}

// ----------------------------- Filters --------------------------------------

type pipe struct {
	name string
	args []string
}

func (p pipe) apply(ctx *renderCtx, in string) (string, error) {
	f := ctx.filters[p.name]
	if f == nil {
		return "", fmt.Errorf("unknown filter %q", p.name)
	}
	return f(in, p.args)
}

func DefaultFilters() Filters {
	return Filters{
		"upper": func(s string, _ []string) (string, error) { return strings.ToUpper(s), nil },
		"lower": func(s string, _ []string) (string, error) { return strings.ToLower(s), nil },
		"trim":  func(s string, _ []string) (string, error) { return fastTrim(s), nil },
		"truncate": func(s string, args []string) (string, error) {
			if len(args) == 0 {
				return s, nil
			}
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 0 {
				return s, nil
			}
			if len(s) <= n {
				return s, nil
			}
			return s[:n], nil
		},
		"replace": func(s string, args []string) (string, error) {
			if len(args) < 2 {
				return s, nil
			}
			return strings.ReplaceAll(s, args[0], args[1]), nil
		},
		"length": func(s string, _ []string) (string, error) {
			return strconv.Itoa(len(s)), nil
		},
	}
}

// ----------------------------- Additional optimizations ------------------

// ByteBuffer provides a zero-allocation byte buffer for template rendering
type ByteBuffer struct {
	buf []byte
}

var byteBufferPool = sync.Pool{
	New: func() any {
		return &ByteBuffer{buf: make([]byte, 0, 1024)}
	},
}

// RenderToBytes renders template to a byte slice without allocations
func (t *Template) RenderToBytes(data any) ([]byte, error) {
	bb := byteBufferPool.Get().(*ByteBuffer)
	bb.buf = bb.buf[:0] // reset length but keep capacity
	defer byteBufferPool.Put(bb)

	err := t.Render((*byteWriter)(bb), data)
	if err != nil {
		return nil, err
	}

	// Return copy of the bytes
	result := make([]byte, len(bb.buf))
	copy(result, bb.buf)
	return result, nil
}

// byteWriter implements io.Writer for ByteBuffer
type byteWriter ByteBuffer

func (bw *byteWriter) Write(p []byte) (n int, err error) {
	bw.buf = append(bw.buf, p...)
	return len(p), nil
}

// ----------------------------- Fast path optimizations -------------------

// Common interface for known types to avoid reflection
type FastAccessor interface {
	GetField(name string) (any, bool)
}

// FastStruct can be implemented by structs for zero-allocation field access
type FastStruct interface {
	FastGet(fieldName string) (any, bool)
}

// Optimized field step for types that implement FastStruct
type fastFieldStep struct{ name string }

func (s fastFieldStep) next(in any) (any, bool) {
	if fs, ok := in.(FastStruct); ok {
		return fs.FastGet(s.name)
	}
	// Fallback to reflection
	return fieldStep{name: s.name}.next(in)
}
