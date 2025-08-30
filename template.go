package fasttpl

import (
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// ----------------------------- Public API -----------------------------------

type Template struct {
	root       node
	parts      map[string]*Template
	filt       Filters
	fieldCache *fieldCache
}

// NewTemplate creates a new template engine that loads all templates from the specified directory
func NewTemplate(dir, ext string, opts ...EngineOption) (*Engine, error) {
	eo := EngineOptions{
		reloadInterval: 1 * time.Second,
	}
	for _, o := range opts {
		o(&eo)
	}

	engine := &Engine{
		templates:     make(map[string]*Template),
		defaultLayout: eo.defaultLayout,
		dir:           dir,
		ext:           ext,
		reloadManager: NewReloadManager(eo.reloadInterval),
	}

	// Load initial templates
	if err := engine.Load(); err != nil {
		return nil, err
	}

	// Set up reload callback
	engine.reloadManager.AddCallback(func(filename string, template *Template, err error) {
		if err != nil {
			// Log error but don't fail
			return
		}
		// Update the template in the engine
		engine.mu.Lock()
		// Extract template name from filename
		base := filepath.Base(filename)
		name := strings.TrimSuffix(base, ext)
		engine.templates[name] = template
		engine.mu.Unlock()
	})

	// Start watching the directory
	if err := engine.reloadManager.WatchDirectory(dir); err != nil {
		return nil, fmt.Errorf("failed to watch directory: %w", err)
	}

	// Start the reload manager
	engine.reloadManager.Start()

	return engine, nil
}

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
	case withNode:
		t.precomputeAccessor(node.acc, dataType)
		t.precomputeNode(node.body, dataType)
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

// RenderToDiscard renders template to io.Discard for benchmarking
func (t *Template) RenderToDiscard(data any) error {
	return t.Render(io.Discard, data)
}
