package fasttpl

import (
	"reflect"
	"strings"
)

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
