package ir

import "fmt"

// Parse parses a rule expression into the IR.
func Parse(s string) (Node, error) {
	toks, err := lexAll(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.cur().Kind != tEOF {
		return nil, fmt.Errorf("unexpected token %q after expression", p.cur().Text)
	}
	return n, nil
}

func lexAll(s string) ([]token, error) {
	l := newLexer(s)
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.Kind == tEOF {
			return toks, nil
		}
	}
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) cur() token {
	if p.i >= len(p.toks) {
		return token{Kind: tEOF}
	}
	return p.toks[p.i]
}

func (p *parser) advance() token {
	t := p.cur()
	p.i++
	return t
}

func (p *parser) expect(k tokenKind, what string) (token, error) {
	if p.cur().Kind != k {
		return token{}, fmt.Errorf("expected %s, got %q", what, p.cur().Text)
	}
	return p.advance(), nil
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	if p.cur().Kind != tOr {
		return left, nil
	}
	args := []Node{left}
	for p.cur().Kind == tOr {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		args = append(args, right)
	}
	return Logic{Op: "OR", Args: args}, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.cur().Kind != tAnd {
		return left, nil
	}
	args := []Node{left}
	for p.cur().Kind == tAnd {
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		args = append(args, right)
	}
	return Logic{Op: "AND", Args: args}, nil
}

func (p *parser) parseUnary() (Node, error) {
	// Prefix NOT: `NOT (expr)`, `NOT field = v`, `NOT NOT x`. It binds tighter
	// than AND/OR but looser than a predicate, so it wraps whatever unary
	// operand follows. `field NOT IN/LIKE` is handled inside parsePredicate.
	if p.cur().Kind == tNot {
		p.advance()
		arg, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return Not{Arg: arg}, nil
	}
	if p.cur().Kind == tLParen {
		p.advance()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "')'"); err != nil {
			return nil, err
		}
		return n, nil
	}
	return p.parsePredicate()
}

func (p *parser) parsePredicate() (Node, error) {
	id, err := p.expect(tIdent, "field identifier")
	if err != nil {
		return nil, err
	}
	field := id.Text

	// Optional NOT before IN / LIKE: `field NOT IN (...)`, `field NOT LIKE '...'`.
	negate := false
	if p.cur().Kind == tNot {
		p.advance()
		negate = true
		if k := p.cur().Kind; k != tIn && k != tLike {
			return nil, fmt.Errorf("expected IN or LIKE after NOT for field %q, got %q", field, p.cur().Text)
		}
	}

	switch p.cur().Kind {
	case tOp:
		op := p.advance().Text
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return Compare{Field: field, Op: op, Val: v}, nil

	case tBetween:
		p.advance()
		lo, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tAnd, "AND in BETWEEN"); err != nil {
			return nil, err
		}
		hi, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return Between{Field: field, Lo: lo, Hi: hi}, nil

	case tIn:
		p.advance()
		if _, err := p.expect(tLParen, "'(' after IN"); err != nil {
			return nil, err
		}
		var vals []Value
		for {
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
			if p.cur().Kind == tComma {
				p.advance()
				continue
			}
			break
		}
		if _, err := p.expect(tRParen, "')' after IN list"); err != nil {
			return nil, err
		}
		return In{Field: field, Vals: vals, Negate: negate}, nil

	case tLike:
		p.advance()
		s, err := p.expect(tString, "string after LIKE")
		if err != nil {
			return nil, err
		}
		return Like{Field: field, Pattern: s.Text, Negate: negate}, nil

	case tIs:
		p.advance()
		negate := false
		if p.cur().Kind == tNot {
			p.advance()
			negate = true
		}
		if _, err := p.expect(tNull, "NULL after IS"); err != nil {
			return nil, err
		}
		return IsNull{Field: field, Negate: negate}, nil

	default:
		return nil, fmt.Errorf("expected operator/BETWEEN/IN/LIKE after %q, got %q", field, p.cur().Text)
	}
}

func (p *parser) parseValue() (Value, error) {
	switch p.cur().Kind {
	case tNumber:
		return Value{Num: p.advance().Text}, nil
	case tString:
		return Value{IsString: true, Str: p.advance().Text}, nil
	default:
		return Value{}, fmt.Errorf("expected value, got %q", p.cur().Text)
	}
}
