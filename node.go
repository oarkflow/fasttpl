package fasttpl

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
)

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

func (n withNode) render(ctx *renderCtx, w io.Writer) error {
	v, ok := n.acc.get(ctx)
	if !ok {
		return nil
	}
	originalData := ctx.data
	ctx.data = v
	defer func() { ctx.data = originalData }()
	return n.body.render(ctx, w)
}

type withNode struct {
	acc  accessor
	body node
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
