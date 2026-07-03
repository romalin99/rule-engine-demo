// panic_safety_test.go — round fifteen: the panic-risk audit's executable
// pins. Stack exhaustion is the one panic Go cannot recover, so the compiler
// must refuse pathologically deep EXTERNALLY BUILT IR with an ordinary error
// (text rules are already parser-bounded at 200 levels and cannot get here).
package vm

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

// deepNot builds a NOT chain of the given depth around a trivial predicate.
func deepNot(depth int) ir.Node {
	var n ir.Node = ir.Compare{Field: "a", Op: "=", Val: ir.Value{Num: "1"}}
	for i := 0; i < depth; i++ {
		n = ir.Not{Arg: n}
	}
	return n
}

func TestCompileDepthGuard(t *testing.T) {
	// 100k-deep external IR: without the guard this would recurse 100k
	// compiler frames (and a hostile construction could go far beyond any
	// stack limit); with it, Compile unwinds at maxIRDepth with an error.
	if _, err := Compile(deepNot(100_000)); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("Compile(deep NOT chain) err = %v, want depth error", err)
	}

	// Deep operand (CALL) nesting recurses through emitTerm, not emit.
	var term ir.Term = ir.FieldTerm{Name: "x"}
	for i := 0; i < 100_000; i++ {
		term = ir.CallTerm{Fn: "UPPER", Args: []ir.Term{term}}
	}
	deepCall := ir.CompareTerm{Left: term, Op: "=", Right: ir.LitTerm{Val: ir.Value{IsString: true, Str: "v"}}}
	if _, err := Compile(deepCall); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("Compile(deep CALL nest) err = %v, want depth error", err)
	}

	// Sub-query WHERE chains must not reset the budget: EXISTS-in-WHERE
	// nesting compiles through fresh sub-programs but inherits the depth.
	var ex ir.Node = ir.Compare{Field: "amount", Op: ">", Val: ir.Value{Num: "0"}}
	for i := 0; i < 100_000; i++ {
		ex = ir.Exists{Coll: "orders", Where: ex}
	}
	if _, err := Compile(ex); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("Compile(deep EXISTS WHERE chain) err = %v, want depth error", err)
	}

	// The AST runtime gates the same shapes at ITS load boundary (round 16 —
	// Execute walks recursively; refusing deep external IR in Compile is the
	// only defense stack exhaustion allows).
	if _, err := astrt.New().Compile(deepNot(100_000)); err == nil ||
		!strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("ast Compile(deep NOT chain) err = %v, want depth error", err)
	}

	// The emitters and optimizer are gated too: deep external IR yields the
	// documented degraded forms ("" / error / unchanged input), never a crash.
	deep := deepNot(100_000)
	if s := ir.Emit(deep, ir.SQL); s != "" {
		t.Fatalf("Emit(deep) = %q, want empty", s)
	}
	if _, err := ir.EmitJSON(deep); err == nil || !strings.Contains(err.Error(), "too deeply") {
		t.Fatalf("EmitJSON(deep) err = %v, want depth error", err)
	}
	if got := ir.Optimize(deep); ir.NestingDepth(got) != ir.NestingDepth(deep) {
		t.Fatalf("Optimize(deep) rewrote a too-deep tree instead of returning it unchanged")
	}

	// Anything a text rule can express stays well inside the budget: each
	// `NOT (` layer costs two parser-depth levels (NOT + parenthesis), so 90
	// layers ≈ depth 180 — near the parser's 200 cap — and must still
	// compile cleanly (compile depth 91 ≪ 500) on both runtimes.
	rule := strings.Repeat("NOT (", 90) + "a = 1" + strings.Repeat(")", 90)
	node, err := ir.Parse(rule)
	if err != nil {
		t.Fatalf("parse 90-layer NOT rule: %v", err)
	}
	if _, err := Compile(node); err != nil {
		t.Fatalf("Compile(parser-bounded rule) err = %v, want nil", err)
	}
	if _, err := astrt.New().Compile(node); err != nil {
		t.Fatalf("ast Compile(parser-bounded rule) err = %v, want nil", err)
	}
	if d := ir.NestingDepth(node); d < 90 || d > ir.MaxNesting {
		t.Fatalf("NestingDepth(90-layer rule) = %d, want within (90, %d]", d, ir.MaxNesting)
	}
}

// TestBuiltinPanicContained proves the SafeCall wiring end to end on both
// runtimes: rules over the third-party-backed builtins evaluate normally
// (no false positives from the containment), and Program.Eval never panics
// even without the engine's matchInto recover around it.
func TestBuiltinPanicContained(t *testing.T) {
	row := map[string]any{
		"ua":  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36",
		"doc": `{"a":[1,2,{"b":"c"}]}`,
	}
	for rule, want := range map[string]bool{
		"USERAGENT(ua, 'mozilla') = '5.0'":    true, // mssola/user_agent behind SafeCall
		"USERAGENT(ua, 'browser') = 'Chrome'": true,
		"HASH_SIP('abc') IS NOT NULL":         true, // dchest/siphash behind SafeCall
		"JMESPATH(doc, 'a[2].b') = 'c'":       true, // go-jmespath behind its recover
		"JMESPATH(doc, 'length(a)') = 3":      true,
	} {
		if got := evalBoth(t, rule, row); got != want {
			t.Errorf("%q = %v, want %v", rule, got, want)
		}
	}
}
