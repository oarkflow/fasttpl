package fasttpl

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unsafe"
)

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
