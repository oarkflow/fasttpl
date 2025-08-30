package fasttpl

import (
	"errors"
	"fmt"
	"strings"
)

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
	case "with":
		rest := fastTrim(strings.TrimPrefix(tag, "with"))
		acc, _, err := compileAccessor(rest)
		if err != nil {
			return nil, err
		}
		bodyNodes, err := p.parseUntilEnd()
		if err != nil {
			return nil, err
		}
		return withNode{acc: acc, body: sequence(bodyNodes)}, nil
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
