// jmes.go — the shared JMESPath evaluator behind JMESPATH / JSON_JMESPATH
// (qlbridge: json.jmespath). Unlike the registry builtins, JMESPath needs the
// raw JSON document (a JSON string or an already-decoded object), so the VM
// and the AST runtime lower it specially (like JSON_EXTRACT) and call into
// these helpers rather than going through Builtin.Fn.
package sqlfn

import (
	"encoding/json"
	"sync"

	"github.com/jmespath/go-jmespath"
)

// jmesCache memoizes compiled JMESPath expressions across calls.
var jmesCache sync.Map

// JmesCompile validates (and caches) a JMESPath expression. Both runtimes call
// it at rule-load time so a bad expression fails the rule's compilation
// instead of silently yielding NULL per row.
func JmesCompile(expr string) error {
	_, err := jmesCompiled(expr)
	return err
}

func jmesCompiled(expr string) (*jmespath.JMESPath, error) {
	if v, ok := jmesCache.Load(expr); ok {
		return v.(*jmespath.JMESPath), nil
	}
	jp, err := jmespath.Compile(expr)
	if err != nil {
		return nil, err
	}
	jmesCache.Store(expr, jp)
	return jp, nil
}

// JmesEval evaluates a JMESPath expression against a raw document value: a
// JSON string is parsed; an already-decoded map/slice is searched as-is.
// Results map into the value model: string / bool pass through, any numeric
// kind becomes float64, an all-scalar array becomes []string (so the result
// composes with ARRAY_CONTAINS / ARRAY_LENGTH / ...), and anything else —
// missing paths, objects, mixed arrays, bad documents — is NULL.
//
// Search runs inside a recover: go-jmespath is a third-party evaluator fed
// row-data document shapes, and a panic on some pathological document must
// degrade to NULL (fail-safe no-match) rather than reach a worker goroutine —
// Program.Eval has no recover of its own.
func JmesEval(raw any, expr string) (out any) {
	root := jmesRoot(raw)
	if root == nil {
		return nil
	}
	jp, err := jmesCompiled(expr)
	if err != nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	res, err := jp.Search(root)
	if err != nil {
		return nil
	}
	return jmesValue(res)
}

// jmesRoot returns a searchable document: a parsed JSON string, or a
// pre-decoded object/array as-is.
func jmesRoot(raw any) any {
	switch x := raw.(type) {
	case string:
		var v any
		if json.Unmarshal([]byte(x), &v) != nil {
			return nil
		}
		return v
	case map[string]any:
		return x
	case []any:
		return x
	default:
		return nil
	}
}

// jmesValue converts a JMESPath result into the value model.
func jmesValue(res any) any {
	switch x := res.(type) {
	case nil:
		return nil
	case bool:
		return x
	case string:
		return x
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
		return nil
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			s, ok := jmesScalarText(e)
			if !ok { // nested object/array element: not an array of scalars
				return nil
			}
			out[i] = s
		}
		return out
	default:
		return nil
	}
}

// jmesScalarText renders a scalar array element as text.
func jmesScalarText(e any) (string, bool) {
	switch v := jmesValue(e).(type) {
	case string:
		return v, true
	case float64:
		return formatNum(v), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}
