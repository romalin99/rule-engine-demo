package vm

import "strconv"

// vkind is the dynamic type tag of a VM stack value.
type vkind uint8

const (
	kUndef vkind = iota // missing field / unknown
	kNum
	kStr
	kBool
	kArr    // array of strings (backs the ARRAY_* functions)
	kOpaque // present but non-scalar: a JSON object / typed row collection
)

// Value is a tagged union held on the VM stack. Using a flat struct (no
// interfaces) keeps evaluation allocation-free and avoids reflection.
type Value struct {
	s   string
	arr []string
	n   float64
	k   vkind
	b   bool
}

var undef = Value{k: kUndef}

func numV(f float64) Value  { return Value{k: kNum, n: f} }
func strV(s string) Value   { return Value{k: kStr, s: s} }
func boolV(b bool) Value    { return Value{k: kBool, b: b} }
func arrV(a []string) Value { return Value{k: kArr, arr: a} }

// toValue converts a raw field value (from map[string]any) into a VM Value.
// JSON numbers arrive as float64; the generator uses int/float64 natively.
func toValue(raw any) Value {
	switch x := raw.(type) {
	case nil:
		return undef
	case bool:
		return boolV(x)
	case string:
		return strV(x)
	case int:
		return numV(float64(x))
	case int8:
		return numV(float64(x))
	case int16:
		return numV(float64(x))
	case int32:
		return numV(float64(x))
	case int64:
		return numV(float64(x))
	case uint:
		return numV(float64(x))
	case uint8:
		return numV(float64(x))
	case uint16:
		return numV(float64(x))
	case uint32:
		return numV(float64(x))
	case uint64:
		return numV(float64(x))
	case float32:
		return numV(float64(x))
	case float64:
		return numV(x)
	case []string:
		return arrV(x)
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = toValue(e).asString()
		}
		return arrV(out)
	case map[string]any, []map[string]any:
		// A pre-parsed JSON object or a typed nested-row collection IS a
		// present value — `profile IS NOT NULL` must hold when the row carries
		// a decoded object (it already held for the same document as a JSON
		// string). It is just not scalar: every comparison / IN / LIKE /
		// REGEXP / scalar function treats kOpaque like NULL, and the JSON /
		// sub-query operators keep reading the raw field directly. (Mapping
		// these to kUndef, as before round thirteen, made `profile IS NULL`
		// true on the VM while the AST runtime said false.)
		return Value{k: kOpaque}
	default:
		return undef
	}
}

// asString renders a value as text (for string comparisons / IN / LIKE).
func (v Value) asString() string {
	switch v.k {
	case kStr:
		return v.s
	case kNum:
		return strconv.FormatFloat(v.n, 'f', -1, 64)
	case kBool:
		if v.b {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// eq reports value equality (numeric, boolean, or string).
func eq(a, b Value) bool {
	if a.k == kNum && b.k == kNum {
		return a.n == b.n
	}
	if a.k == kBool && b.k == kBool {
		return a.b == b.b
	}
	return a.asString() == b.asString()
}
