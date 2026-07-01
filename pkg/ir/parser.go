package ir

import (
	"fmt"
	"strings"
)

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
	// EXISTS is a prefix predicate; `NOT EXISTS(...)` arrives here via the NOT
	// branch above wrapping this in a Not node.
	if p.cur().Kind == tExists {
		return p.parseExists()
	}
	if p.cur().Kind == tLParen {
		p.advance()
		// `( SELECT AGG(...) FROM coll [WHERE ...] )` is a scalar aggregate operand,
		// not a grouped boolean expression; parse it then the comparison around it.
		if p.cur().Kind == tSelect {
			left, err := p.parseAggSub()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tRParen, "')' after scalar sub-query"); err != nil {
				return nil, err
			}
			return p.parseTermPredicate(left)
		}
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

	// Function call on the left operand → function-aware comparison, e.g.
	// `LOWER(name) = 'abc'`, `ABS(balance) >= 100`. A bare identifier immediately
	// followed by '(' is only valid here (it was a parse error before), so this
	// branch is purely additive and never changes how existing rules parse.
	if p.cur().Kind == tLParen {
		left, err := p.finishCall(field)
		if err != nil {
			return nil, err
		}
		// Boolean-valued array predicates are complete on their own (no operator
		// follows): ARRAY_CONTAINS becomes a PredCall; ARRAY_OVERLAP desugars to
		// an OR of ARRAY_CONTAINS, so it needs no dedicated opcode.
		if call, ok := left.(CallTerm); ok {
			switch call.Fn {
			case "ARRAY_CONTAINS":
				return PredCall(call), nil
			case "ARRAY_OVERLAP":
				return overlapToOr(call.Args)
			case "ARRAY_INTERSECT":
				return PredCall(call), nil
			case "REGEXP_LIKE":
				return regexpLikeToNode(call.Args)
			}
		}
		return p.parseTermPredicate(left)
	}

	// CURRENT_DATE / CURRENT_TIMESTAMP are nullary date functions written without
	// parentheses. As the LEFT operand of a predicate they must resolve to the
	// function value (today / now), not a field literally named "CURRENT_DATE";
	// route them through the term-based predicate tail so that
	// `CURRENT_DATE >= last_login`, `CURRENT_TIMESTAMP < x`, etc. evaluate the
	// function. This mirrors parseTerm's handling of the same keywords on the
	// right-hand side.
	if u := upper(field); u == "CURRENT_DATE" || u == "CURRENT_TIMESTAMP" {
		return p.parseTermPredicate(CallTerm{Fn: u})
	}

	// Optional NOT before IN / LIKE / REGEXP: `field NOT IN (...)`,
	// `field NOT LIKE '...'`, `field NOT REGEXP '...'`.
	negate := false
	if p.cur().Kind == tNot {
		p.advance()
		negate = true
		if k := p.cur().Kind; k != tIn && k != tLike && k != tRegexp {
			return nil, fmt.Errorf("expected IN, LIKE or REGEXP after NOT for field %q, got %q", field, p.cur().Text)
		}
	}

	switch p.cur().Kind {
	case tOp:
		op := p.advance().Text
		// `field <op> ANY|ALL ( … )` — quantified comparison / sub-query.
		if k := p.cur().Kind; k == tAny || k == tAll {
			return p.parseQuant(FieldTerm{Name: field}, op, k == tAll)
		}
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		// Keep the efficient legacy node for `field <op> literal`; fall back to
		// the term-based comparison when the right side is a field or function
		// call (e.g. `last_login >= CURRENT_DATE`, `a = b`).
		if lit, ok := right.(LitTerm); ok {
			return Compare{Field: field, Op: op, Val: lit.Val}, nil
		}
		return CompareTerm{Left: FieldTerm{Name: field}, Op: op, Right: right}, nil

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
		// Numeric bounds keep the compact numeric-range Between node. String bounds
		// (e.g. date ranges like `register_date BETWEEN '2020-01-01' AND
		// '2020-12-31'`) desugar into `field >= lo AND field <= hi`; both runtimes
		// evaluate string comparisons with lexical ordering, which equals
		// chronological order for ISO date strings. This also keeps the two
		// runtimes consistent (the numeric-only Between path used to fail to
		// compile on the bytecode VM and silently return false on the AST runtime).
		if lo.IsString || hi.IsString {
			return Logic{Op: "AND", Args: []Node{
				Compare{Field: field, Op: ">=", Val: lo},
				Compare{Field: field, Op: "<=", Val: hi},
			}}, nil
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

	case tRegexp:
		p.advance()
		s, err := p.expect(tString, "string after REGEXP")
		if err != nil {
			return nil, err
		}
		return Regexp{Left: FieldTerm{Name: field}, Pattern: s.Text, Negate: negate}, nil

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

// parseTerm parses a scalar operand: a literal, a field, or a function call.
// It is the operand grammar shared by both sides of a CompareTerm.
func (p *parser) parseTerm() (Term, error) {
	switch p.cur().Kind {
	case tNumber:
		return LitTerm{Val: Value{Num: p.advance().Text}}, nil
	case tString:
		return LitTerm{Val: Value{IsString: true, Str: p.advance().Text}}, nil
	case tIdent:
		name := p.advance().Text
		if p.cur().Kind == tLParen {
			return p.finishCall(name)
		}
		// CURRENT_DATE / CURRENT_TIMESTAMP are nullary functions written without
		// parentheses; everything else is a field reference.
		if u := upper(name); u == "CURRENT_DATE" || u == "CURRENT_TIMESTAMP" {
			return CallTerm{Fn: u}, nil
		}
		return FieldTerm{Name: name}, nil
	case tLParen:
		// A parenthesised scalar operand is a scalar aggregate sub-query, e.g.
		// `budget >= (SELECT SUM(amount) FROM orders)`.
		p.advance()
		t, err := p.parseAggSub()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "')' after scalar sub-query"); err != nil {
			return nil, err
		}
		return t, nil
	default:
		return nil, fmt.Errorf("expected value, field or function, got %q", p.cur().Text)
	}
}

// finishCall parses the `( arg, arg, ... )` of a function call whose name has
// already been consumed. Argument count/typing is validated later, at compile.
func (p *parser) finishCall(name string) (Term, error) {
	if _, err := p.expect(tLParen, "'(' after function name"); err != nil {
		return nil, err
	}
	var args []Term
	if p.cur().Kind != tRParen {
		for {
			arg, err := p.parseTerm()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.cur().Kind == tComma {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(tRParen, "')' after function arguments"); err != nil {
		return nil, err
	}
	return CallTerm{Fn: upper(name), Args: args}, nil
}

// parseTermPredicate parses the predicate tail after a function-call left
// operand: comparison, BETWEEN, IN/NOT IN, LIKE/NOT LIKE, or IS [NOT] NULL.
// BETWEEN and IN desugar into CompareTerm logic, so they need no new IR nodes.
func (p *parser) parseTermPredicate(left Term) (Node, error) {
	negate := false
	if p.cur().Kind == tNot {
		p.advance()
		negate = true
		if k := p.cur().Kind; k != tIn && k != tLike && k != tRegexp {
			return nil, fmt.Errorf("expected IN, LIKE or REGEXP after NOT, got %q", p.cur().Text)
		}
	}

	switch p.cur().Kind {
	case tOp:
		op := p.advance().Text
		// `f(x) <op> ANY|ALL ( … )` — quantified comparison with a function left.
		if k := p.cur().Kind; k == tAny || k == tAll {
			return p.parseQuant(left, op, k == tAll)
		}
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		return CompareTerm{Left: left, Op: op, Right: right}, nil

	case tBetween:
		p.advance()
		lo, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tAnd, "AND in BETWEEN"); err != nil {
			return nil, err
		}
		hi, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		// desugar `t BETWEEN lo AND hi` -> `t >= lo AND t <= hi`
		return Logic{Op: "AND", Args: []Node{
			CompareTerm{Left: left, Op: ">=", Right: lo},
			CompareTerm{Left: left, Op: "<=", Right: hi},
		}}, nil

	case tIn:
		p.advance()
		if _, err := p.expect(tLParen, "'(' after IN"); err != nil {
			return nil, err
		}
		var vals []Term
		for {
			v, err := p.parseTerm()
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
		// desugar `t IN (v...)` -> OR of equals; `t NOT IN (v...)` -> AND of !=
		op, logicOp := "=", "OR"
		if negate {
			op, logicOp = "!=", "AND"
		}
		args := make([]Node, len(vals))
		for i, v := range vals {
			args[i] = CompareTerm{Left: left, Op: op, Right: v}
		}
		if len(args) == 1 {
			return args[0], nil
		}
		return Logic{Op: logicOp, Args: args}, nil

	case tLike:
		p.advance()
		s, err := p.expect(tString, "string after LIKE")
		if err != nil {
			return nil, err
		}
		return LikeTerm{Left: left, Pattern: s.Text, Negate: negate}, nil

	case tRegexp:
		p.advance()
		s, err := p.expect(tString, "string after REGEXP")
		if err != nil {
			return nil, err
		}
		return Regexp{Left: left, Pattern: s.Text, Negate: negate}, nil

	case tIs:
		p.advance()
		neg := false
		if p.cur().Kind == tNot {
			p.advance()
			neg = true
		}
		if _, err := p.expect(tNull, "NULL after IS"); err != nil {
			return nil, err
		}
		return IsNullTerm{Left: left, Negate: neg}, nil

	default:
		return nil, fmt.Errorf("expected operator/BETWEEN/IN/LIKE/IS after function call, got %q", p.cur().Text)
	}
}

// overlapToOr desugars ARRAY_OVERLAP(arr, v1, v2, ...) into an OR of
// ARRAY_CONTAINS(arr, vi) predicates, so it needs no dedicated opcode.
func overlapToOr(args []Term) (Node, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("ARRAY_OVERLAP expects an array and at least one value")
	}
	arr := args[0]
	preds := make([]Node, 0, len(args)-1)
	for _, v := range args[1:] {
		preds = append(preds, PredCall{Fn: "ARRAY_CONTAINS", Args: []Term{arr, v}})
	}
	if len(preds) == 1 {
		return preds[0], nil
	}
	return Logic{Op: "OR", Args: preds}, nil
}

// regexpLikeToNode desugars REGEXP_LIKE(expr, pattern[, match_type]) into a
// Regexp node. pattern must be a string literal; a match_type containing 'i'
// makes the match case-insensitive (folded into the pattern as a `(?i)` flag).
func regexpLikeToNode(args []Term) (Node, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("REGEXP_LIKE expects (expr, pattern[, match_type])")
	}
	lit, ok := args[1].(LitTerm)
	if !ok || !lit.Val.IsString {
		return nil, fmt.Errorf("REGEXP_LIKE pattern must be a string literal")
	}
	pat := lit.Val.Str
	if len(args) == 3 {
		if mt, ok := args[2].(LitTerm); ok && mt.Val.IsString && strings.Contains(mt.Val.Str, "i") {
			pat = "(?i)" + pat
		}
	}
	return Regexp{Left: args[0], Pattern: pat}, nil
}

// parseExists parses `EXISTS ( coll )` or `EXISTS ( SELECT … FROM coll [WHERE
// pred] )`. The EXISTS keyword has already been peeked (not consumed).
func (p *parser) parseExists() (Node, error) {
	p.advance() // EXISTS
	if _, err := p.expect(tLParen, "'(' after EXISTS"); err != nil {
		return nil, err
	}
	var node Node
	if p.cur().Kind == tSelect {
		_, coll, where, err := p.parseSubSelect()
		if err != nil {
			return nil, err
		}
		node = Exists{Coll: coll, Where: where}
	} else {
		id, err := p.expect(tIdent, "collection field or SELECT after 'EXISTS('")
		if err != nil {
			return nil, err
		}
		node = Exists{Coll: id.Text}
	}
	if _, err := p.expect(tRParen, "')' to close EXISTS(...)"); err != nil {
		return nil, err
	}
	return node, nil
}

// parseQuant parses the right-hand side of `left <op> ANY|ALL ( … )`. The body
// is one of: a sub-query (`SELECT col FROM coll [WHERE pred]`), a single bare
// array field (runtime element expansion), or a value list (desugared to an
// OR/AND of comparisons). The ANY/ALL keyword has been peeked (not consumed).
func (p *parser) parseQuant(left Term, op string, all bool) (Node, error) {
	p.advance() // ANY / ALL / SOME
	if op == "==" { // normalize to '=' so both runtimes compare identically
		op = "="
	}
	if _, err := p.expect(tLParen, "'(' after ANY/ALL"); err != nil {
		return nil, err
	}

	// Sub-query form: ANY|ALL ( SELECT col FROM coll [WHERE pred] ).
	if p.cur().Kind == tSelect {
		col, coll, where, err := p.parseSubSelect()
		if err != nil {
			return nil, err
		}
		if col == "" {
			return nil, fmt.Errorf("ANY/ALL sub-query must project a column: SELECT col FROM ...")
		}
		if _, err := p.expect(tRParen, "')' to close ANY/ALL sub-query"); err != nil {
			return nil, err
		}
		return QuantSub{Left: left, Op: op, All: all, Col: col, Coll: coll, Where: where}, nil
	}

	// Otherwise a comma-separated term list.
	var items []Term
	for {
		t, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		items = append(items, t)
		if p.cur().Kind == tComma {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(tRParen, "')' to close ANY/ALL list"); err != nil {
		return nil, err
	}

	// A single bare field reference ranges over that array field's elements.
	if len(items) == 1 {
		if ft, ok := items[0].(FieldTerm); ok {
			return QuantArr{Left: left, Op: op, All: all, Array: ft}, nil
		}
	}

	// A value list desugars: ANY -> OR of (left op v); ALL -> AND of (left op v).
	logicOp := "OR"
	if all {
		logicOp = "AND"
	}
	args := make([]Node, len(items))
	for i, v := range items {
		args[i] = CompareTerm{Left: left, Op: op, Right: v}
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return Logic{Op: logicOp, Args: args}, nil
}

// parseSubSelect parses the body `SELECT (col | 1 | *) FROM coll [WHERE pred]`
// of a sub-query (the opening '(' is consumed by the caller, the closing ')' is
// left for the caller). It returns the projected column (empty for `*` / `1`),
// the source collection field, and the optional WHERE predicate over nested
// rows. Identifiers inside the WHERE predicate resolve to nested-row fields.
func (p *parser) parseSubSelect() (col, coll string, where Node, err error) {
	if _, err = p.expect(tSelect, "SELECT"); err != nil {
		return
	}
	switch p.cur().Kind {
	case tStar:
		p.advance() // SELECT * — projection irrelevant (EXISTS)
	case tNumber:
		p.advance() // SELECT 1 — projection irrelevant (EXISTS)
	case tIdent:
		col = p.advance().Text
	default:
		err = fmt.Errorf("expected column, '*' or '1' after SELECT, got %q", p.cur().Text)
		return
	}
	if _, err = p.expect(tFrom, "FROM in sub-query"); err != nil {
		return
	}
	id, e := p.expect(tIdent, "collection field after FROM")
	if e != nil {
		err = e
		return
	}
	coll = id.Text
	if p.cur().Kind == tWhere {
		p.advance()
		where, err = p.parseOr() // predicate evaluated against each nested row
	}
	return
}

// parseAggSub parses the body `SELECT <AGG>(col|*) FROM coll [WHERE pred]` of a
// scalar aggregate sub-query (the surrounding parentheses are handled by the
// caller). AGG is COUNT/SUM/MIN/MAX/AVG; only COUNT accepts `*`.
func (p *parser) parseAggSub() (Term, error) {
	if _, err := p.expect(tSelect, "SELECT"); err != nil {
		return nil, err
	}
	fnTok, err := p.expect(tIdent, "aggregate function COUNT/SUM/MIN/MAX/AVG")
	if err != nil {
		return nil, err
	}
	fn := upper(fnTok.Text)
	switch fn {
	case "COUNT", "SUM", "MIN", "MAX", "AVG":
	default:
		return nil, fmt.Errorf("unsupported aggregate %q (want COUNT/SUM/MIN/MAX/AVG)", fnTok.Text)
	}
	if _, err := p.expect(tLParen, "'(' after aggregate function"); err != nil {
		return nil, err
	}
	col := ""
	if p.cur().Kind == tStar {
		if fn != "COUNT" {
			return nil, fmt.Errorf("%s requires a column, not '*'", fn)
		}
		p.advance()
	} else {
		colTok, err := p.expect(tIdent, "column name or '*'")
		if err != nil {
			return nil, err
		}
		col = colTok.Text
	}
	if _, err := p.expect(tRParen, "')' after aggregate argument"); err != nil {
		return nil, err
	}
	if _, err := p.expect(tFrom, "FROM in aggregate sub-query"); err != nil {
		return nil, err
	}
	collTok, err := p.expect(tIdent, "collection field after FROM")
	if err != nil {
		return nil, err
	}
	var where Node
	if p.cur().Kind == tWhere {
		p.advance()
		where, err = p.parseOr()
		if err != nil {
			return nil, err
		}
	}
	return AggSub{Fn: fn, Col: col, Coll: collTok.Text, Where: where}, nil
}
