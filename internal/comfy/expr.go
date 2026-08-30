package comfy

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// evalExpr evaluates the small arithmetic language used by ComfyMathExpression
// nodes, with the graph-resolved values of a/b/c bound in vars.
//
// Supported: + - * / % ** ( ) unary minus, and the functions max, min, round,
// floor, ceil, abs, int, sqrt, pow. That covers the frame-snapping expression
// the MiniMax H3 workflows rely on:
//
//	max(5, round(a * 24)) + (5 - (max(5, round(a * 24)) % 17)) % 17
func evalExpr(src string, vars map[string]float64) (float64, error) {
	toks, err := tokenize(src)
	if err != nil {
		return 0, err
	}
	p := &exprParser{toks: toks, vars: vars}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.toks) {
		return 0, fmt.Errorf("trailing input at token %d", p.pos)
	}
	return v, nil
}

type token struct {
	kind string // "num", "ident", "op"
	text string
	num  float64
}

func tokenize(src string) ([]token, error) {
	var out []token
	rs := []rune(src)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case unicode.IsDigit(r) || r == '.':
			j := i
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.') {
				j++
			}
			f, err := strconv.ParseFloat(string(rs[i:j]), 64)
			if err != nil {
				return nil, fmt.Errorf("bad number %q", string(rs[i:j]))
			}
			out = append(out, token{kind: "num", num: f})
			i = j
		case unicode.IsLetter(r) || r == '_':
			j := i
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			out = append(out, token{kind: "ident", text: string(rs[i:j])})
			i = j
		case strings.ContainsRune("+-*/%(),", r):
			if r == '*' && i+1 < len(rs) && rs[i+1] == '*' {
				out = append(out, token{kind: "op", text: "**"})
				i += 2
				continue
			}
			out = append(out, token{kind: "op", text: string(r)})
			i++
		default:
			return nil, fmt.Errorf("unexpected character %q", string(r))
		}
	}
	return out, nil
}

type exprParser struct {
	toks []token
	pos  int
	vars map[string]float64
}

func (p *exprParser) peek() (token, bool) {
	if p.pos >= len(p.toks) {
		return token{}, false
	}
	return p.toks[p.pos], true
}

func (p *exprParser) acceptOp(ops ...string) (string, bool) {
	t, ok := p.peek()
	if !ok || t.kind != "op" {
		return "", false
	}
	for _, op := range ops {
		if t.text == op {
			p.pos++
			return op, true
		}
	}
	return "", false
}

// parseExpr handles the lowest precedence level: addition and subtraction.
func (p *exprParser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		op, ok := p.acceptOp("+", "-")
		if !ok {
			return left, nil
		}
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == "+" {
			left += right
		} else {
			left -= right
		}
	}
}

func (p *exprParser) parseTerm() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		op, ok := p.acceptOp("*", "/", "%")
		if !ok {
			return left, nil
		}
		right, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		switch op {
		case "*":
			left *= right
		case "/":
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		case "%":
			if right == 0 {
				return 0, fmt.Errorf("modulo by zero")
			}
			left = pyMod(left, right)
		}
	}
}

// pyMod reproduces Python's floor modulo, which is what ComfyMathExpression
// nodes evaluate. Go's math.Mod truncates instead, so a negative intermediate
// such as (5 - 24%17) would come out as -2 rather than 15 and the frame count
// would land off the 17k+5 grid.
func pyMod(a, b float64) float64 {
	m := math.Mod(a, b)
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

func (p *exprParser) parseUnary() (float64, error) {
	if _, ok := p.acceptOp("-"); ok {
		v, err := p.parseUnary()
		return -v, err
	}
	if _, ok := p.acceptOp("+"); ok {
		return p.parseUnary()
	}
	return p.parsePower()
}

func (p *exprParser) parsePower() (float64, error) {
	base, err := p.parseAtom()
	if err != nil {
		return 0, err
	}
	if _, ok := p.acceptOp("**"); ok {
		exp, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *exprParser) parseAtom() (float64, error) {
	t, ok := p.peek()
	if !ok {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	switch t.kind {
	case "num":
		p.pos++
		return t.num, nil
	case "ident":
		p.pos++
		if next, ok := p.peek(); ok && next.kind == "op" && next.text == "(" {
			args, err := p.parseArgs()
			if err != nil {
				return 0, err
			}
			return callFunc(t.text, args)
		}
		v, ok := p.vars[t.text]
		if !ok {
			return 0, fmt.Errorf("unbound variable %q", t.text)
		}
		return v, nil
	case "op":
		if t.text == "(" {
			p.pos++
			v, err := p.parseExpr()
			if err != nil {
				return 0, err
			}
			if _, ok := p.acceptOp(")"); !ok {
				return 0, fmt.Errorf("missing closing parenthesis")
			}
			return v, nil
		}
	}
	return 0, fmt.Errorf("unexpected token %q", t.text)
}

func (p *exprParser) parseArgs() ([]float64, error) {
	if _, ok := p.acceptOp("("); !ok {
		return nil, fmt.Errorf("expected (")
	}
	var args []float64
	if _, ok := p.acceptOp(")"); ok {
		return args, nil
	}
	for {
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		if _, ok := p.acceptOp(","); ok {
			continue
		}
		if _, ok := p.acceptOp(")"); ok {
			return args, nil
		}
		return nil, fmt.Errorf("expected , or ) in argument list")
	}
}

func callFunc(name string, args []float64) (float64, error) {
	need := func(n int) error {
		if len(args) != n {
			return fmt.Errorf("%s expects %d argument(s), got %d", name, n, len(args))
		}
		return nil
	}
	switch strings.ToLower(name) {
	case "max":
		if len(args) == 0 {
			return 0, fmt.Errorf("max expects arguments")
		}
		v := args[0]
		for _, a := range args[1:] {
			v = math.Max(v, a)
		}
		return v, nil
	case "min":
		if len(args) == 0 {
			return 0, fmt.Errorf("min expects arguments")
		}
		v := args[0]
		for _, a := range args[1:] {
			v = math.Min(v, a)
		}
		return v, nil
	case "round":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Round(args[0]), nil
	case "floor", "int":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Floor(args[0]), nil
	case "ceil":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Ceil(args[0]), nil
	case "abs":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Abs(args[0]), nil
	case "sqrt":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Sqrt(args[0]), nil
	case "pow":
		if err := need(2); err != nil {
			return 0, err
		}
		return math.Pow(args[0], args[1]), nil
	}
	return 0, fmt.Errorf("unknown function %q", name)
}
