// Package sqlfn is the extended scalar-function library shared by both rule
// runtimes (the bytecode VM and the tree-walking AST interpreter). It brings
// the engine to parity with the practical computable factors of
// github.com/araddon/qlbridge's builtin set that the core opcodes did not
// already cover: string predicates and transforms (CONTAINS / STARTSWITH /
// SPLIT / REPLACE / JOIN / ...), math (SQRT / POW), type casts (TOINT /
// TONUMBER / TOBOOL / TOSTRING), extra date extractors (NOW / TODATE /
// TOTIMESTAMP / DAYOFWEEK / HOUROFDAY / ...), email and URL accessors,
// hashes (MD5 / SHA1 / SHA256 / SHA512), base64, ONEOF/COALESCE and
// positional array access (ARRAY_INDEX / ARRAY_SLICE).
//
// Value model (mirrors the VM's Value kinds):
//
//	nil        NULL (missing field / failed computation)
//	float64    number
//	string     text
//	bool       boolean (result of the predicate-style functions)
//	[]string   array (elements as text, matching the VM's kArr)
//
// Every function is total: it never panics and never returns an error — an
// invalid or NULL input yields nil (SQL NULL propagation), and the boolean
// predicates yield false. Argument counts are validated once at compile time
// via Builtin.ArityOK, not per call.
//
// The registry is append-only and registration order fixes each builtin's
// numeric ID (the operand of the VM's OpCallB instruction). IDs are stable
// within a process — programs are compiled in-process at rule-load time and
// never serialized — so the order of the register* calls in init matters only
// for that stability, not for correctness.
package sqlfn

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// Builtin describes one extended function: its arity bounds, whether it is
// boolean-valued (usable as a standalone predicate), and its implementation.
type Builtin struct {
	Fn   func([]any) any
	Name string
	Min  int
	Max  int
	ID   int32
	Bool bool
}

// ArityOK reports whether n arguments satisfy the builtin's arity bounds.
func (b *Builtin) ArityOK(n int) bool {
	return n >= b.Min && (b.Max < 0 || n <= b.Max)
}

// ArityDoc renders the arity bounds for error messages: "2", "1-2" or "2+".
func (b *Builtin) ArityDoc() string {
	switch {
	case b.Max < 0:
		return strconv.Itoa(b.Min) + "+"
	case b.Min == b.Max:
		return strconv.Itoa(b.Min)
	default:
		return strconv.Itoa(b.Min) + "-" + strconv.Itoa(b.Max)
	}
}

var (
	byName = map[string]*Builtin{}
	byID   []*Builtin
)

// register adds one implementation under one or more names. Each alias gets
// its own registry entry (and ID) sharing the same implementation.
func register(names []string, minArgs, maxArgs int, isBool bool, fn func([]any) any) {
	for _, n := range names {
		b := &Builtin{Name: n, ID: int32(len(byID)), Min: minArgs, Max: maxArgs, Bool: isBool, Fn: fn}
		byName[n] = b
		byID = append(byID, b)
	}
}

// Lookup resolves a function name (case-insensitive) to its builtin.
func Lookup(name string) (*Builtin, bool) {
	b, ok := byName[strings.ToUpper(name)]
	return b, ok
}

// ByID returns the builtin registered under an OpCallB operand ID, or nil.
func ByID(id int32) *Builtin {
	if id < 0 || int(id) >= len(byID) {
		return nil
	}
	return byID[id]
}

// IsBoolFn reports whether name is a boolean-valued extended builtin (one that
// forms a complete predicate on its own, like CONTAINS or STARTSWITH).
func IsBoolFn(name string) bool {
	b, ok := Lookup(name)
	return ok && b.Bool
}

// recoveredPanics counts builtin invocations whose implementation panicked and
// was contained by SafeCall (each one is a bug — the contract says builtins
// are total — but it must never cost the process).
var recoveredPanics atomic.Int64

// RecoveredPanics reports how many builtin panics SafeCall has contained since
// process start (export it next to the engine's EvalPanics; non-zero values
// deserve an alert).
func RecoveredPanics() int64 { return recoveredPanics.Load() }

// SafeCall invokes a builtin with panic containment: a panicking
// implementation yields NULL, exactly like every other invalid input. This is
// the single choke point both runtimes call through (the VM's OpCallB and the
// AST runtime's applyBuiltin), so the 130+ registered implementations —
// including the third-party-backed ones (USERAGENT via mssola/user_agent,
// HASH_SIP via dchest/siphash) — can never take a worker down, even when
// Program.Eval is used directly without the engine's matchInto recover.
// The defer is open-coded by the compiler (single defer, no loop), so the
// no-panic fast path costs ~1 ns on top of an already-allocating call.
func SafeCall(b *Builtin, args []any) (out any) {
	defer func() {
		if recover() != nil {
			recoveredPanics.Add(1)
			out = nil
		}
	}()
	return b.Fn(args)
}

// Names returns every registered function name (for docs / editor completion).
func Names() []string {
	out := make([]string, len(byID))
	for i, b := range byID {
		out[i] = b.Name
	}
	return out
}

func init() {
	registerStrings()
	registerCompare()
	registerNumbers()
	registerDates()
	registerNet()
	registerHashes()
	registerArrays()
	registerDates2()
	registerNet2()
	registerParity()
	registerSQLPlus() // round fourteen: common-SQL additions — keep LAST so
	// every previously registered builtin keeps its OpCallB ID
}

// ---- shared coercions -------------------------------------------------------

// str renders a scalar as text. Numbers and booleans render like the VM's
// Value.asString; nil and arrays are not strings.
func str(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return formatNum(x), true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	}
	return "", false
}

// num extracts a numeric operand (the value model carries numbers as float64).
func num(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// arr extracts an array operand.
func arr(v any) ([]string, bool) {
	a, ok := v.([]string)
	return a, ok
}

// formatNum renders a float64 the way the VM does (no trailing zeros).
func formatNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseFloat parses a decimal string, reporting success instead of an error.
func parseFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// formatUint renders a uint64 as decimal text (used by HASH_SIP, whose 64-bit
// results do not all fit exactly in the value model's float64).
func formatUint(u uint64) string {
	return strconv.FormatUint(u, 10)
}
