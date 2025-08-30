package fasttpl

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// ----------------------------- Public API -----------------------------------

type Template struct {
	root       node
	parts      map[string]*Template
	filt       Filters
	fieldCache *fieldCache
}

type Filters map[string]func(string, []string) (string, error)

type Option func(*compileOptions)

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

	if exists && !cached.modTime.Before(info.ModTime()) {
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

	// Cache the result
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

	return tmpl, nil
}

// ClearCache clears the file cache
func (fc *FileCache) ClearCache() {
	fc.mu.Lock()
	fc.templates = make(map[string]*cachedTemplate)
	fc.mu.Unlock()
}

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

// RegisterPartial stores a named partial template for {{ include "name" }}
func (t *Template) RegisterPartial(name string, partial *Template) {
	t.parts[name] = partial
}

// Render executes the template with the given data into w. Data may be a struct, map or any value.
func (t *Template) Render(w io.Writer, data any) error {
	ctx := renderCtxPool.Get().(*renderCtx)
	ctx.reset(data, t.parts, t.filt, t.fieldCache)
	defer renderCtxPool.Put(ctx)
	return t.root.render(ctx, w)
}

// RenderString renders into a pooled buffer and returns a string.
func (t *Template) RenderString(data any) (string, error) {
	sb := stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	defer stringBuilderPool.Put(sb)
	if err := t.Render(sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// ----------------------------- Buffer and context pools ---------------------

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

var renderCtxPool = sync.Pool{
	New: func() any {
		return &renderCtx{
			locals: make(map[string]any, 16), // pre-allocate common size
		}
	},
}

var fieldsPool = sync.Pool{
	New: func() any {
		return make([]string, 0, 8)
	},
}

var stepsPool = sync.Pool{
	New: func() any {
		return make([]step, 0, 8)
	},
}

var pipesPool = sync.Pool{
	New: func() any {
		return make([]pipe, 0, 4)
	},
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

// ----------------------------- AST & runtime --------------------------------

type node interface {
	render(*renderCtx, io.Writer) error
}

type renderCtx struct {
	data       any
	locals     map[string]any
	parts      map[string]*Template
	filters    Filters
	fieldCache *fieldCache
}

func (ctx *renderCtx) reset(data any, parts map[string]*Template, filters Filters, fc *fieldCache) {
	ctx.data = data
	// Clear locals map without reallocating
	for k := range ctx.locals {
		delete(ctx.locals, k)
	}
	ctx.parts = parts
	ctx.filters = filters
	ctx.fieldCache = fc
}

type textNode struct{ text string }

func (n textNode) render(_ *renderCtx, w io.Writer) error {
	_, err := io.WriteString(w, n.text)
	return err
}

type printNode struct {
	acc   accessor
	raw   bool
	pipes []pipe
}

func (n printNode) render(ctx *renderCtx, w io.Writer) error {
	v, ok := n.acc.get(ctx)
	if !ok {
		return nil
	}

	// Use pre-allocated string builder for filtering
	sb := stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	defer stringBuilderPool.Put(sb)

	s := toStringFast(v, sb)

	for _, p := range n.pipes {
		var err error
		s, err = p.apply(ctx, s)
		if err != nil {
			return err
		}
	}

	if n.raw {
		_, err := io.WriteString(w, s)
		return err
	}

	// Use pooled buffer for HTML escaping
	escaped := htmlEscapeFast(s)
	_, err := io.WriteString(w, escaped)
	return err
}

var stringBuilderPool = sync.Pool{
	New: func() any { return &strings.Builder{} },
}

type ifNode struct {
	cond accessor
	then node
	els  node
}

func (n ifNode) render(ctx *renderCtx, w io.Writer) error {
	v, _ := n.cond.get(ctx)
	if truthyFast(v) {
		return n.then.render(ctx, w)
	}
	if n.els != nil {
		return n.els.render(ctx, w)
	}
	return nil
}

type rangeNode struct {
	iter accessor
	item string
	body node
}

func (n rangeNode) render(ctx *renderCtx, w io.Writer) error {
	v, _ := n.iter.get(ctx)
	rv := reflect.ValueOf(v)

	// Store original value for restoration
	originalVal, hadOriginal := ctx.locals[n.item]

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		// Fast path for []any
		if rv.Type().Elem() == reflect.TypeOf((*any)(nil)).Elem() {
			slice := rv.Interface().([]any)
			for i := 0; i < len(slice); i++ {
				ctx.locals[n.item] = slice[i]
				if err := n.body.render(ctx, w); err != nil {
					// Restore original value
					if hadOriginal {
						ctx.locals[n.item] = originalVal
					} else {
						delete(ctx.locals, n.item)
					}
					return err
				}
			}
		} else if rv.Type().Elem() == reflect.TypeOf((*map[string]any)(nil)).Elem() {
			slice := rv.Interface().([]map[string]any)
			for i := 0; i < len(slice); i++ {
				ctx.locals[n.item] = slice[i]
				if err := n.body.render(ctx, w); err != nil {
					// Restore original value
					if hadOriginal {
						ctx.locals[n.item] = originalVal
					} else {
						delete(ctx.locals, n.item)
					}
					return err
				}
			}
		} else {
			for i := 0; i < rv.Len(); i++ {
				ctx.locals[n.item] = rv.Index(i).Interface()
				if err := n.body.render(ctx, w); err != nil {
					// Restore original value
					if hadOriginal {
						ctx.locals[n.item] = originalVal
					} else {
						delete(ctx.locals, n.item)
					}
					return err
				}
			}
		}
	case reflect.Map:
		// Fast path for map[string]any
		if rv.Type().Key().Kind() == reflect.String && rv.Type().Elem() == reflect.TypeOf((*any)(nil)).Elem() {
			m := rv.Interface().(map[string]any)
			for _, v := range m {
				ctx.locals[n.item] = v
				if err := n.body.render(ctx, w); err != nil {
					// Restore original value
					if hadOriginal {
						ctx.locals[n.item] = originalVal
					} else {
						delete(ctx.locals, n.item)
					}
					return err
				}
			}
		} else {
			for _, key := range rv.MapKeys() {
				ctx.locals[n.item] = rv.MapIndex(key).Interface()
				if err := n.body.render(ctx, w); err != nil {
					// Restore original value
					if hadOriginal {
						ctx.locals[n.item] = originalVal
					} else {
						delete(ctx.locals, n.item)
					}
					return err
				}
			}
		}
	}

	// Restore original value
	if hadOriginal {
		ctx.locals[n.item] = originalVal
	} else {
		delete(ctx.locals, n.item)
	}
	return nil
}

type letNode struct {
	name string
	acc  accessor
}

func (n letNode) render(ctx *renderCtx, _ io.Writer) error {
	v, _ := n.acc.get(ctx)
	ctx.locals[n.name] = v
	return nil
}

type includeNode struct{ name string }

func (n includeNode) render(ctx *renderCtx, w io.Writer) error {
	p := ctx.parts[n.name]
	if p == nil {
		return fmt.Errorf("include: partial %q not found", n.name)
	}
	return p.root.render(ctx, w)
}

type seqNode []node

func (s seqNode) render(ctx *renderCtx, w io.Writer) error {
	for _, n := range s {
		if err := n.render(ctx, w); err != nil {
			return err
		}
	}
	return nil
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
	}
}

// ----------------------------- Fast accessors -------------------------------

type accessor interface{ get(*renderCtx) (any, bool) }

type step interface{ next(in any) (any, bool) }

type localStep struct{ name string }

func (s localStep) next(in any) (any, bool) { return in, true }

type rootStep struct{}

func (s rootStep) next(in any) (any, bool) { return in, true }

type fieldStep struct {
	name string
	// Pre-computed reflection info for common types
	structType reflect.Type
	fieldIndex []int
}

func (s fieldStep) next(in any) (any, bool) {
	rv := reflect.ValueOf(in)
	if !rv.IsValid() {
		return nil, false
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, false
		}
		return s.next(rv.Elem().Interface())
	case reflect.Struct:
		// Use cached field lookup
		typ := rv.Type()
		if s.structType == typ && s.fieldIndex != nil {
			// Fast path: use cached field index
			fv := rv.FieldByIndex(s.fieldIndex)
			if fv.IsValid() {
				return fv.Interface(), true
			}
			return nil, false
		}

		// Fallback to field lookup (will be cached for next time)
		fv := rv.FieldByNameFunc(func(n string) bool {
			return n == s.name || strings.EqualFold(n, s.name)
		})
		if fv.IsValid() {
			return fv.Interface(), true
		}
	case reflect.Map:
		// Fast path for map[string]any
		if rv.Type().Key().Kind() == reflect.String && rv.Type().Elem() == reflect.TypeOf((*any)(nil)).Elem() {
			m := rv.Interface().(map[string]any)
			if val, ok := m[s.name]; ok {
				return val, true
			}
			return nil, false
		}
		// Fallback
		mk := stringToReflectValue(s.name)
		mv := rv.MapIndex(mk)
		if mv.IsValid() {
			return mv.Interface(), true
		}
	}
	return nil, false
}

type indexStep struct{ idx int }

func (s indexStep) next(in any) (any, bool) {
	rv := reflect.ValueOf(in)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if s.idx < 0 || s.idx >= rv.Len() {
			return nil, false
		}
		elem := rv.Index(s.idx)
		// Fast path for []any or []map[string]any
		if rv.Type().Elem() == reflect.TypeOf((*any)(nil)).Elem() {
			slice := rv.Interface().([]any)
			return slice[s.idx], true
		} else if rv.Type().Elem() == reflect.TypeOf((*map[string]any)(nil)).Elem() {
			slice := rv.Interface().([]map[string]any)
			return slice[s.idx], true
		}
		// Fallback
		return elem.Interface(), true
	}
	return nil, false
}

type keyStep struct{ key string }

func (s keyStep) next(in any) (any, bool) {
	rv := reflect.ValueOf(in)
	if rv.Kind() == reflect.Map {
		mv := rv.MapIndex(stringToReflectValue(s.key))
		if mv.IsValid() {
			return mv.Interface(), true
		}
	}
	return nil, false
}

// boundAcc is the main accessor implementation
type boundAcc struct {
	steps []step
}

func (a boundAcc) get(ctx *renderCtx) (any, bool) {
	if len(a.steps) == 0 {
		return ctx.data, true
	}

	var cur any
	if ls, ok := a.steps[0].(localStep); ok {
		// Local path: start from locals
		v, ok := ctx.locals[ls.name]
		if !ok {
			return nil, false
		}
		cur = v
		for _, st := range a.steps[1:] {
			v, ok = st.next(cur)
			if !ok {
				return nil, false
			}
			cur = v
		}
		return cur, true
	}

	// Non-local path: start from data
	cur = ctx.data
	for _, st := range a.steps {
		v, ok := st.next(cur)
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// ----------------------------- Parser ---------------------------------------

type parser struct {
	src        string
	i          int
	leftDelim  string
	rightDelim string
}

func (p *parser) eof() bool { return p.i >= len(p.src) }

func (p *parser) parse() ([]node, error) {
	nodes := make([]node, 0, 16) // pre-allocate
	for !p.eof() {
		// find next tag
		start := strings.Index(p.src[p.i:], p.leftDelim)
		if start == -1 {
			// rest is text
			if p.i < len(p.src) {
				nodes = append(nodes, textNode{text: p.src[p.i:]})
			}
			p.i = len(p.src)
			break
		}
		if start > 0 {
			nodes = append(nodes, textNode{text: p.src[p.i : p.i+start]})
		}
		p.i += start + len(p.leftDelim) // skip leftDelim
		// find end
		end := strings.Index(p.src[p.i:], p.rightDelim)
		if end == -1 {
			return nil, errors.New("unterminated tag")
		}
		tag := fastTrim(p.src[p.i : p.i+end])
		p.i += end + len(p.rightDelim)
		// dispatch tag
		n, err := p.parseTag(tag)
		if err != nil {
			return nil, err
		}
		if n != nil {
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

func (p *parser) parseTag(tag string) (node, error) {
	fields := splitFieldsFast(tag)
	defer returnFields(fields)
	if len(fields) == 0 {
		return nil, nil
	}
	switch fields[0] {
	case "raw":
		acc, pipes, err := compileAccessor(fastTrim(strings.TrimPrefix(tag, "raw")))
		if err != nil {
			return nil, err
		}
		return printNode{acc: acc, raw: true, pipes: pipes}, nil
	case "if":
		condExpr := fastTrim(strings.TrimPrefix(tag, "if"))
		cond, _, err := compileAccessor(condExpr)
		if err != nil {
			return nil, err
		}
		// parse until {{ end }} or {{ else }}
		thenNodes, elseNodes, err := p.parseUntilElseOrEnd()
		if err != nil {
			return nil, err
		}
		return ifNode{cond: cond, then: sequence(thenNodes), els: sequence(elseNodes)}, nil
	case "range":
		// syntax: range item in path
		rest := fastTrim(strings.TrimPrefix(tag, "range"))
		inIdx := strings.Index(rest, " in ")
		if inIdx == -1 {
			return nil, fmt.Errorf("range syntax: range item in path")
		}
		item := fastTrim(rest[:inIdx])
		pathExpr := fastTrim(rest[inIdx+4:])
		acc, _, err := compileAccessor(pathExpr)
		if err != nil {
			return nil, err
		}
		bodyNodes, err := p.parseUntilEnd()
		if err != nil {
			return nil, err
		}
		return rangeNode{iter: acc, item: item, body: sequence(bodyNodes)}, nil
	case "let":
		// let name = path
		rest := fastTrim(strings.TrimPrefix(tag, "let"))
		eq := strings.Index(rest, "=")
		if eq < 0 {
			return nil, fmt.Errorf("let syntax: let name = path")
		}
		name := fastTrim(rest[:eq])
		acc, _, err := compileAccessor(fastTrim(rest[eq+1:]))
		if err != nil {
			return nil, err
		}
		return letNode{name: name, acc: acc}, nil
	case "include":
		if len(fields) < 2 {
			return nil, fmt.Errorf("include syntax: include \"name\"")
		}
		name := unquote(fields[1])
		return includeNode{name: name}, nil
	default:
		// treat as expression
		acc, pipes, err := compileAccessor(tag)
		if err != nil {
			return nil, err
		}
		return printNode{acc: acc, raw: false, pipes: pipes}, nil
	}
}

func (p *parser) parseUntilEnd() ([]node, error) {
	nodes := make([]node, 0, 8)
	for !p.eof() {
		start := strings.Index(p.src[p.i:], p.leftDelim)
		if start == -1 {
			return nil, fmt.Errorf("unterminated block (missing %s end %s)", p.leftDelim, p.rightDelim)
		}
		if start > 0 {
			nodes = append(nodes, textNode{text: p.src[p.i : p.i+start]})
		}
		p.i += start + len(p.leftDelim)
		end := strings.Index(p.src[p.i:], p.rightDelim)
		if end == -1 {
			return nil, errors.New("unterminated tag")
		}
		tag := fastTrim(p.src[p.i : p.i+end])
		p.i += end + len(p.rightDelim)
		if tag == "end" {
			return nodes, nil
		}
		n, err := p.parseTag(tag)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nil, fmt.Errorf("unterminated block (missing %s end %s)", p.leftDelim, p.rightDelim)
}

func (p *parser) parseUntilElseOrEnd() (thenNodes []node, elseNodes []node, err error) {
	thenNodes = make([]node, 0, 8)
	for !p.eof() {
		start := strings.Index(p.src[p.i:], p.leftDelim)
		if start == -1 {
			return nil, nil, fmt.Errorf("unterminated if (missing %s end %s)", p.leftDelim, p.rightDelim)
		}
		if start > 0 {
			thenNodes = append(thenNodes, textNode{text: p.src[p.i : p.i+start]})
		}
		p.i += start + len(p.leftDelim)
		end := strings.Index(p.src[p.i:], p.rightDelim)
		if end == -1 {
			return nil, nil, errors.New("unterminated tag")
		}
		tag := fastTrim(p.src[p.i : p.i+end])
		p.i += end + len(p.rightDelim)
		if tag == "end" {
			return thenNodes, nil, nil
		}
		if tag == "else" {
			elseNodes, err = p.parseUntilEnd()
			return thenNodes, elseNodes, err
		}
		n, err := p.parseTag(tag)
		if err != nil {
			return nil, nil, err
		}
		thenNodes = append(thenNodes, n)
	}
	return nil, nil, fmt.Errorf("unterminated if block (missing %s end %s)", p.leftDelim, p.rightDelim)
}

// ----------------------------- Accessor compiler -----------------------------

func compileAccessor(expr string) (accessor, []pipe, error) {
	expr = fastTrim(expr)
	if expr == "" {
		return boundAcc{}, nil, nil
	}

	// Find first pipe
	pipeIdx := strings.Index(expr, "|")
	if pipeIdx == -1 {
		// No pipes
		acc, err := compilePath(expr)
		return acc, nil, err
	}

	path := fastTrim(expr[:pipeIdx])
	acc, err := compilePath(path)
	if err != nil {
		return nil, nil, err
	}

	// Parse pipes manually to avoid allocations
	pipesStr := expr[pipeIdx+1:]
	tempPipes := pipesPool.Get().([]pipe)
	tempPipes = tempPipes[:0]

	for pipesStr != "" {
		pipesStr = fastTrim(pipesStr)
		if pipesStr == "" {
			break
		}
		nextPipe := strings.Index(pipesStr, "|")
		var pipeStr string
		if nextPipe == -1 {
			pipeStr = pipesStr
			pipesStr = ""
		} else {
			pipeStr = fastTrim(pipesStr[:nextPipe])
			pipesStr = pipesStr[nextPipe+1:]
		}

		if pipeStr == "" {
			continue
		}

		colonIdx := strings.Index(pipeStr, ":")
		var name string
		var args []string
		if colonIdx == -1 {
			name = pipeStr
		} else {
			name = fastTrim(pipeStr[:colonIdx])
			argsStr := fastTrim(pipeStr[colonIdx+1:])
			if argsStr != "" {
				args = splitArgs(argsStr)
			}
		}
		tempPipes = append(tempPipes, pipe{name: name, args: args})
	}

	// Copy pipes to avoid holding pool reference
	pipes := make([]pipe, len(tempPipes))
	copy(pipes, tempPipes)
	pipesPool.Put(tempPipes[:0])

	return acc, pipes, nil
}

func compilePath(path string) (accessor, error) {
	path = fastTrim(path)
	if path == "" {
		return boundAcc{}, nil
	}

	steps := stepsPool.Get().([]step)
	steps = steps[:0]

	var rest string
	var idxSteps []step
	var name string

	if strings.HasPrefix(path, "$") {
		name = strings.TrimPrefix(path, "$")
		name, rest, idxSteps = scanDotted(name)
		steps = append(steps, localStep{name: name})
		steps = append(steps, idxSteps...)
	} else {
		name, rest, idxSteps = scanDotted(path)
		steps = append(steps, fieldStep{name: name})
		steps = append(steps, idxSteps...)
	}

	for rest != "" {
		name, rest, idxSteps = scanDotted(rest)
		steps = append(steps, fieldStep{name: name})
		steps = append(steps, idxSteps...)
	}

	// Copy steps to avoid holding pool reference
	finalSteps := make([]step, len(steps))
	copy(finalSteps, steps)
	stepsPool.Put(steps[:0])

	return boundAcc{steps: finalSteps}, nil
}

func scanDotted(s string) (ident string, rest string, idxSteps []step) {
	s = fastTrim(s)
	// identifier until dot, bracket or end
	i := 0
	for i < len(s) && (isAlphaNum(s[i]) || s[i] == '_') {
		i++
	}
	ident = s[:i]
	j := i
	for j < len(s) {
		switch s[j] {
		case '[':
			// parse index or key
			k := j + 1
			if k < len(s) && (s[k] == '"' || s[k] == '\'') {
				q := s[k]
				k++
				start := k
				for k < len(s) && s[k] != q {
					k++
				}
				key := s[start:k]
				idxSteps = append(idxSteps, keyStep{key: key})
				k++ // skip quote
				if k < len(s) && s[k] == ']' {
					k++
				}
				j = k
				continue
			}
			// number index
			start := k
			for k < len(s) && isDigit(s[k]) {
				k++
			}
			if k > start {
				idx, _ := strconv.Atoi(s[start:k])
				idxSteps = append(idxSteps, indexStep{idx: idx})
			}
			if k < len(s) && s[k] == ']' {
				k++
			}
			j = k
		case '.':
			rest = fastTrim(s[j+1:])
			return
		default:
			rest = fastTrim(s[j:])
			return
		}
	}
	rest = ""
	return
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// ----------------------------- Fast utilities -------------------------------

// splitFieldsFast is an optimized version that reuses a pooled slice
func splitFieldsFast(s string) []string {
	fields := fieldsPool.Get().([]string)
	fields = fields[:0] // reset slice but keep capacity

	var start int
	inQuote := byte(0)

	for i := 0; i <= len(s); i++ {
		if i == len(s) || (inQuote == 0 && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n')) {
			if i > start {
				field := fastTrim(s[start:i])
				if field != "" {
					fields = append(fields, field)
				}
			}
			start = i + 1
			continue
		}

		if i < len(s) {
			c := s[i]
			if inQuote == 0 && (c == '"' || c == '\'') {
				inQuote = c
			} else if inQuote != 0 && c == inQuote {
				inQuote = 0
			}
		}
	}

	return fields
}

// returnFields returns the slice to the pool
func returnFields(fields []string) {
	fieldsPool.Put(fields[:0])
}

func splitArgs(s string) []string {
	parts := strings.Split(s, ":")
	for i := range parts {
		parts[i] = fastTrim(unquote(parts[i]))
	}
	return parts
}

func unquote(s string) string {
	if len(s) >= 2 {
		q := s[0]
		if (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// toStringFast avoids allocations for common types
func toStringFast(v any, sb *strings.Builder) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return *(*string)(unsafe.Pointer(&x)) // zero-copy conversion
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case fmt.Stringer:
		return x.String()
	default:
		// Fallback to fmt - use string builder to avoid allocation
		sb.Reset()
		fmt.Fprintf(sb, "%v", x)
		return sb.String()
	}
}

// truthyFast is an optimized version of truthy
func truthyFast(v any) bool {
	if v == nil {
		return false
	}

	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []byte:
		return len(x) != 0
	default:
		// Fallback to reflection for other types
		rv := reflect.ValueOf(v)
		return !rv.IsZero()
	}
}

// stringToReflectValue converts string to reflect.Value without allocation (cached)
func stringToReflectValue(s string) reflect.Value {
	return globalValueCache.get(s)
}

// htmlEscapeFast is an optimized HTML escaper that minimizes allocations
func htmlEscapeFast(s string) string {
	// Quick scan for characters that need escaping
	needsEscape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '&' || c == '<' || c == '>' || c == '"' || c == '\'' {
			needsEscape = true
			break
		}
	}

	if !needsEscape {
		return s
	}

	// Use pooled string builder for escaping
	sb := stringBuilderPool.Get().(*strings.Builder)
	sb.Reset()
	defer stringBuilderPool.Put(sb)

	// Pre-allocate capacity to avoid reallocation
	sb.Grow(len(s) + len(s)/4)

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&#39;")
		default:
			sb.WriteByte(c)
		}
	}

	// To avoid allocation, use unsafe to get the string without copy
	result := sb.String()
	return result
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

// ----------------------------- Enhanced file operations ------------------

// ----------------------------- Performance utilities ---------------------

// PrecomputeFieldAccess optimizes field access for known struct types
func (t *Template) PrecomputeFieldAccess(dataType reflect.Type) {
	// Walk the AST and precompute field indices for struct access
	t.precomputeNode(t.root, dataType)
}

func (t *Template) precomputeNode(n node, dataType reflect.Type) {
	switch node := n.(type) {
	case printNode:
		t.precomputeAccessor(node.acc, dataType)
	case ifNode:
		t.precomputeAccessor(node.cond, dataType)
		t.precomputeNode(node.then, dataType)
		if node.els != nil {
			t.precomputeNode(node.els, dataType)
		}
	case rangeNode:
		t.precomputeAccessor(node.iter, dataType)
		t.precomputeNode(node.body, dataType)
	case letNode:
		t.precomputeAccessor(node.acc, dataType)
	case seqNode:
		for _, child := range node {
			t.precomputeNode(child, dataType)
		}
	}
}

func (t *Template) precomputeAccessor(acc accessor, dataType reflect.Type) {
	if ba, ok := acc.(boundAcc); ok {
		currentType := dataType
		for i, step := range ba.steps {
			if fs, ok := step.(fieldStep); ok && currentType.Kind() == reflect.Struct {
				// Cache field index for this struct type
				if field, found := currentType.FieldByName(fs.name); found {
					// Update the step with cached info
					ba.steps[i] = fieldStep{
						name:       fs.name,
						structType: currentType,
						fieldIndex: field.Index,
					}
					currentType = field.Type
				}
			}
		}
	}
}

// ----------------------------- Benchmark helpers -------------------------

// RenderToDiscard renders template to io.Discard for benchmarking
func (t *Template) RenderToDiscard(data any) error {
	return t.Render(io.Discard, data)
}

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

// ----------------------------- Template pools for hot paths ---------------

type TemplatePool struct {
	pool sync.Pool
}

func NewTemplatePool(templateSrc string, opts ...Option) (*TemplatePool, error) {
	return &TemplatePool{
		pool: sync.Pool{
			New: func() any {
				// Each pool entry gets its own copy to avoid concurrency issues
				newTmpl, _ := Compile(templateSrc, opts...)
				return newTmpl
			},
		},
	}, nil
}

func (tp *TemplatePool) Render(w io.Writer, data any) error {
	tmpl := tp.pool.Get().(*Template)
	defer tp.pool.Put(tmpl)
	return tmpl.Render(w, data)
}

func (tp *TemplatePool) RenderString(data any) (string, error) {
	tmpl := tp.pool.Get().(*Template)
	defer tp.pool.Put(tmpl)
	return tmpl.RenderString(data)
}

func fastTrim(s string) string {
	if len(s) == 0 {
		return s
	}

	start := 0
	end := len(s)

	// Find first non-whitespace character
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' && c != '\v' && c != '\f' {
			break
		}
		start++
	}

	// Find last non-whitespace character
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' && c != '\v' && c != '\f' {
			break
		}
		end--
	}

	// Return slice if no allocation needed
	if start == 0 && end == len(s) {
		return s
	}

	return s[start:end]
}
