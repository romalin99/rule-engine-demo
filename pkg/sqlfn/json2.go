// json2.go — shared semantics for the round-fourteen JSON functions
// (JSON_LENGTH / JSON_TYPE / JSON_VALID / JSON_CONTAINS). Like JSON_EXTRACT
// and JMESPATH, these need the RAW document (a JSON string or an
// already-decoded object — the kOpaque row shape), so the bytecode VM and the
// AST runtime lower them specially and both call into this one implementation:
// the runtimes only differ in how they fetch the document, never in what the
// function means.
package sqlfn

import "encoding/json"

// JSONLength returns the MySQL JSON_LENGTH of a decoded document value:
// object -> number of keys, array -> number of elements, scalar -> 1.
// nil (missing path / undecodable document / JSON null) -> NULL. (Deviation:
// MySQL distinguishes the JSON null scalar, which this model cannot.)
func JSONLength(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return float64(len(x))
	case []any:
		return float64(len(x))
	default: // string / float64 / bool / json.Number
		return float64(1)
	}
}

// JSONTypeOf names a decoded JSON value's type: 'OBJECT', 'ARRAY', 'STRING',
// 'NUMBER' (MySQL splits INTEGER/DOUBLE — this model has one number kind),
// 'BOOLEAN'; nil -> NULL.
func JSONTypeOf(v any) any {
	switch v.(type) {
	case nil:
		return nil
	case map[string]any:
		return "OBJECT"
	case []any:
		return "ARRAY"
	case string:
		return "STRING"
	case bool:
		return "BOOLEAN"
	case float64, json.Number:
		return "NUMBER"
	}
	return nil
}

// JSONValid reports whether a raw row value is a JSON document: a string that
// parses as JSON (any JSON value, scalars included — MySQL semantics), or an
// already-decoded object / array / typed nested collection. NULL and scalar
// row values (a number field is not a JSON document) are false.
func JSONValid(raw any) bool {
	switch x := raw.(type) {
	case string:
		return json.Valid([]byte(x))
	case map[string]any, []any, []map[string]any:
		return true
	}
	return false
}

// JSONCandidate converts a candidate argument of JSON_CONTAINS into a decoded
// JSON value: a string that parses as JSON is used decoded (so '["a","b"]'
// and '{"k":1}' work), any other string is a JSON string scalar (deviation
// from MySQL, which would reject the unquoted form — this engine's rules
// write JSON_CONTAINS(tags_json, 'vip') naturally); numbers and booleans map
// to their JSON scalars; a []string array maps to a JSON array of strings.
func JSONCandidate(v any) any {
	switch x := v.(type) {
	case string:
		var decoded any
		if json.Unmarshal([]byte(x), &decoded) == nil {
			return decoded
		}
		return x
	case float64, bool:
		return x
	case []string:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out
	}
	return nil
}

// JSONDeepContains implements MySQL-style JSON_CONTAINS(target, candidate):
//
//   - object ⊇ object: every candidate key exists and its value is contained;
//   - array ⊇ array: every candidate element is contained in the array;
//   - array ∋ scalar/object: some element equals the scalar (top level only —
//     [[1]] does not contain 1, matching MySQL) / contains the object;
//   - scalar vs scalar: equality (numbers numerically).
func JSONDeepContains(target, candidate any) bool {
	if target == nil || candidate == nil {
		return false
	}
	switch t := target.(type) {
	case map[string]any:
		c, ok := candidate.(map[string]any)
		if !ok {
			return false
		}
		for k, cv := range c {
			tv, ok := t[k]
			if !ok || !JSONDeepContains(tv, cv) {
				return false
			}
		}
		return true
	case []any:
		if c, ok := candidate.([]any); ok {
			for _, ce := range c {
				if !jsonArrayHas(t, ce) {
					return false
				}
			}
			return true
		}
		return jsonArrayHas(t, candidate)
	default:
		return jsonScalarEq(target, candidate)
	}
}

// jsonArrayHas reports whether one array element contains x at the top level.
func jsonArrayHas(arr []any, x any) bool {
	for _, e := range arr {
		switch x.(type) {
		case map[string]any:
			if _, ok := e.(map[string]any); ok && JSONDeepContains(e, x) {
				return true
			}
		case []any:
			if _, ok := e.([]any); ok && JSONDeepContains(e, x) {
				return true
			}
		default:
			if jsonScalarEq(e, x) {
				return true
			}
		}
	}
	return false
}

// jsonScalarEq compares two decoded JSON scalars (numbers numerically, so
// json.Number and float64 forms of the same value are equal).
func jsonScalarEq(a, b any) bool {
	if af, ok := jsonNum(a); ok {
		bf, ok := jsonNum(b)
		return ok && af == bf
	}
	switch x := a.(type) {
	case string:
		s, ok := b.(string)
		return ok && x == s
	case bool:
		bb, ok := b.(bool)
		return ok && x == bb
	}
	return false
}

func jsonNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}
