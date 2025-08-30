package fasttpl

import (
	"bytes"
	"io"
	"sync"
)

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
