package vm

import "strconv"

// vkind is the dynamic type tag of a VM stack value.
type vkind uint8

const (
	kUndef vkind = iota // missing field / unknown
	kNum
	kStr
	kBool
)

// Value is a tagged union held on the VM stack. Using a flat struct (no
// interfaces) keeps evaluation allocation-free and avoids reflection.
type Value struct {
	s string
	n float64
	k vkind
	b bool
}

var undef = Value{k: kUndef}

func numV(f float64) Value { return Value{k: kNum, n: f} }
func strV(s string) Value  { return Value{k: kStr, s: s} }
func boolV(b bool) Value   { return Value{k: kBool, b: b} }

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
