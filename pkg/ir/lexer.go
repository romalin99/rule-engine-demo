// Package ir provides an intermediate representation (IR) for the rule
// expression subset, plus a parser and emitters that convert one rule into
// several target DSLs: SQL, Aviator, CEL and Expr (Go).
//
//	SQL WHERE text ─▶ Lexer ─▶ Parser ─▶ IR ─▶ Emit{SQL|Aviator|CEL|Expr}
//
// It is independent of qlbridge: the engine evaluates with qlbridge, while this
// package is a portable translator so the same rule can target multiple engines.
package ir

import (
	"fmt"
	"unicode"
)

// tokenKind enumerates lexical token categories.
type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNumber
	tString
	tOp // = == != >= <= > <
	tLParen
	tRParen
	tComma
	tAnd
	tOr
	tBetween
	tIn
	tLike
	tIs
	tNot
	tNull
)

// token is a single lexical token.
type token struct {
	Text string
	Kind tokenKind
	Pos  int
}

// lexer turns rule text into tokens.
type lexer struct {
	src []rune
	pos int
}

func newLexer(s string) *lexer { return &lexer{src: []rune(s)} }

func (l *lexer) peekRune() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) at(off int) rune {
	i := l.pos + off
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

// next returns the next token, or an error for unexpected input.
func (l *lexer) next() (token, error) {
	// skip whitespace
	for l.pos < len(l.src) && unicode.IsSpace(l.src[l.pos]) {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{Kind: tEOF, Pos: l.pos}, nil
	}

	start := l.pos
	r := l.src[l.pos]

	switch {
	case r == '(':
		l.pos++
		return token{Kind: tLParen, Text: "(", Pos: start}, nil
	case r == ')':
		l.pos++
		return token{Kind: tRParen, Text: ")", Pos: start}, nil
	case r == ',':
		l.pos++
		return token{Kind: tComma, Text: ",", Pos: start}, nil
	case r == '\'' || r == '"':
		return l.lexString(r)
	case r == '=' || r == '!' || r == '>' || r == '<':
		return l.lexOp()
	case unicode.IsDigit(r):
		return l.lexNumber()
	case isIdentStart(r):
		return l.lexIdent()
	default:
		return token{}, fmt.Errorf("unexpected character %q at pos %d", string(r), start)
	}
}

func (l *lexer) lexString(quote rune) (token, error) {
	start := l.pos
	l.pos++ // opening quote
	var sb []rune
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) { // escaped char
			sb = append(sb, l.src[l.pos+1])
			l.pos += 2
			continue
		}
		if c == quote {
			l.pos++ // closing quote
			return token{Kind: tString, Text: string(sb), Pos: start}, nil
		}
		sb = append(sb, c)
		l.pos++
	}
	return token{}, fmt.Errorf("unterminated string at pos %d", start)
}

func (l *lexer) lexOp() (token, error) {
	start := l.pos
	r := l.src[l.pos]
	switch r {
	case '=':
		if l.at(1) == '=' {
			l.pos += 2
			return token{Kind: tOp, Text: "==", Pos: start}, nil
		}
		l.pos++
		return token{Kind: tOp, Text: "=", Pos: start}, nil
	case '!':
		if l.at(1) == '=' {
			l.pos += 2
			return token{Kind: tOp, Text: "!=", Pos: start}, nil
		}
		return token{}, fmt.Errorf("unexpected '!' at pos %d", start)
	case '>':
		if l.at(1) == '=' {
			l.pos += 2
			return token{Kind: tOp, Text: ">=", Pos: start}, nil
		}
		l.pos++
		return token{Kind: tOp, Text: ">", Pos: start}, nil
	case '<':
		if l.at(1) == '=' {
			l.pos += 2
			return token{Kind: tOp, Text: "<=", Pos: start}, nil
		}
		if l.at(1) == '>' { // SQL not-equal: <> is an alias for !=
			l.pos += 2
			return token{Kind: tOp, Text: "!=", Pos: start}, nil
		}
		l.pos++
		return token{Kind: tOp, Text: "<", Pos: start}, nil
	}
	return token{}, fmt.Errorf("bad operator at pos %d", start)
}

func (l *lexer) lexNumber() (token, error) {
	start := l.pos
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if unicode.IsDigit(c) || c == '.' {
			l.pos++
			continue
		}
		break
	}
	return token{Kind: tNumber, Text: string(l.src[start:l.pos]), Pos: start}, nil
}

func (l *lexer) lexIdent() (token, error) {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	text := string(l.src[start:l.pos])
	switch upper(text) {
	case "AND":
		return token{Kind: tAnd, Text: text, Pos: start}, nil
	case "OR":
		return token{Kind: tOr, Text: text, Pos: start}, nil
	case "BETWEEN":
		return token{Kind: tBetween, Text: text, Pos: start}, nil
	case "IN":
		return token{Kind: tIn, Text: text, Pos: start}, nil
	case "LIKE":
		return token{Kind: tLike, Text: text, Pos: start}, nil
	case "IS":
		return token{Kind: tIs, Text: text, Pos: start}, nil
	case "NOT":
		return token{Kind: tNot, Text: text, Pos: start}, nil
	case "NULL":
		return token{Kind: tNull, Text: text, Pos: start}, nil
	}
	return token{Kind: tIdent, Text: text, Pos: start}, nil
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}
	return string(out)
}
