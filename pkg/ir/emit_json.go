package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// EmitJSON renders an IR node as a structured JSON rule document — the inverse
// of engine.JSONFrontend, so a rule round-trips JSON → IR → JSON (and
// SQL → IR → JSON) into text that the JSON front-end re-parses to the same IR.
//
// The JSON rule grammar covers the boolean/predicate subset:
//
//	{"and":[ <node>, … ]}   {"or":[ … ]}   {"not": <node>}
//	{"field":"age","op":"between","values":[25,40]}
//	{"field":"city","op":"in","values":["深圳","广州"]}      // op "not_in" for NOT IN
//	{"field":"name","op":"like","value":"数%"}               // op "not_like" for NOT LIKE
//	{"field":"score","op":">=","value":85}
//	{"field":"phone","op":"isnull"}                          // or "isnotnull"
//
// Constructs with no JSON-rule form — function calls (LOWER/ABS/DATE_ADD/…),
// REGEXP, JSON_EXTRACT, array predicates, EXISTS/ANY/ALL, aggregate sub-queries
// and the function-aware comparison nodes — return an error rather than emit a
// lossy document. (These are exactly the constructs the JSON front-end cannot
// parse either, so the limitation is symmetric.) Callers that want a best-effort
// string can use Emit(n, JSONRule), which drops the error.
func EmitJSON(n Node) (string, error) {
	m, err := jsonNodeOf(n)
	if err != nil {
		return "", err
	}
	// json.Marshal HTML-escapes < > & (`>=` would become `\u003e=`). Rule
	// documents are not HTML: they are stored, diffed and read by people, and
	// the operator set literally contains '>' / '<'. Encode with HTML escaping
	// off so the emitted document uses the same spelling the JSON front-end
	// grammar documents (both forms re-parse identically; this pins the
	// canonical one). Encode appends a trailing newline — strip it.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	return string(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// jsonNodeOf converts one IR node into the map shape that marshals to a JSON
// rule document. It handles exactly the nodes engine.JSONFrontend produces.
func jsonNodeOf(n Node) (map[string]any, error) {
	switch t := n.(type) {
	case Logic:
		key := "and"
		if t.Op == "OR" {
			key = "or"
		}
		children := make([]any, 0, len(t.Args))
		for _, a := range t.Args {
			c, err := jsonNodeOf(a)
			if err != nil {
				return nil, err
			}
			children = append(children, c)
		}
		return map[string]any{key: children}, nil

	case Not:
		c, err := jsonNodeOf(t.Arg)
		if err != nil {
			return nil, err
		}
		return map[string]any{"not": c}, nil

	case Compare:
		return map[string]any{"field": t.Field, "op": t.Op, "value": jsonValueOf(t.Val)}, nil

	case Between:
		return map[string]any{
			"field":  t.Field,
			"op":     "between",
			"values": []any{jsonValueOf(t.Lo), jsonValueOf(t.Hi)},
		}, nil

	case In:
		op := "in"
		if t.Negate {
			op = "not_in"
		}
		vals := make([]any, len(t.Vals))
		for i, v := range t.Vals {
			vals[i] = jsonValueOf(v)
		}
		return map[string]any{"field": t.Field, "op": op, "values": vals}, nil

	case Like:
		op := "like"
		if t.Negate {
			op = "not_like"
		}
		// Legacy (Wildcards=false) patterns are re-encoded so the JSON form —
		// which the JSON front-end re-parses under the wildcard grammar —
		// keeps the node's literal semantics (see likeEmitPattern).
		return map[string]any{"field": t.Field, "op": op, "value": likeEmitPattern(t.Pattern, t.Wildcards)}, nil

	case IsNull:
		op := "isnull"
		if t.Negate {
			op = "isnotnull"
		}
		return map[string]any{"field": t.Field, "op": op}, nil
	}
	return nil, fmt.Errorf("ir: %T has no JSON-rule form (functions, REGEXP, JSON_EXTRACT, arrays, EXISTS/ANY/ALL and aggregate sub-queries are SQL/native-only)", n)
}

// jsonValueOf renders an IR literal for a JSON document: numbers become JSON
// numbers (json.RawMessage of the original text, so 85 stays 85 and 3.5 stays
// 3.5), strings/bools stay their native JSON types. This mirrors engine.
// JSONFrontend's jsonVal, so the value round-trips to the same ir.Value.
func jsonValueOf(v Value) any {
	if v.IsString {
		// A boolean literal stored as a string (the JSON front-end renders bool
		// values that way) round-trips as a JSON string too — matching jsonVal.
		return v.Str
	}
	if _, err := strconv.ParseFloat(v.Num, 64); err == nil {
		return json.RawMessage(v.Num) // preserve integer vs. decimal formatting
	}
	return v.Num // non-numeric (should not happen for a numeric literal)
}
