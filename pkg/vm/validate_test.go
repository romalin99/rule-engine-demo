// validate_test.go — round sixteen: hand-built Program validation. These are
// exactly the shapes security_review §15.3 used to list as a documented
// residual risk: out-of-range pool indices and unbounded sub-program nesting
// now degrade to a Validate error / Eval=false instead of an unrecoverable
// panic on whatever goroutine called Eval.
package vm

import (
	"regexp"
	"strings"
	"testing"
)

func TestValidateHandBuiltPrograms(t *testing.T) {
	row := map[string]any{"x": 1}
	cases := []struct {
		name string
		prog *Program
		want string // substring of the Validate error
	}{
		{"field index OOB", &Program{
			Code: []Instr{{Op: OpLoadField, A: 3}, {Op: OpIsNull}},
		}, "field index 3 out of range"},
		{"negative index", &Program{
			Code: []Instr{{Op: OpConstNum, A: -1}},
		}, "out of range"},
		{"string pool OOB", &Program{
			Code:   []Instr{{Op: OpLoadField, A: 0}, {Op: OpLikePrefix, A: 9}},
			Fields: []string{"x"},
		}, "string const index 9 out of range"},
		{"nil regexp slot", &Program{
			Code:    []Instr{{Op: OpLoadField, A: 0}, {Op: OpRegexp, A: 0}},
			Fields:  []string{"x"},
			Regexps: []*regexp.Regexp{nil},
		}, "nil regexp"},
		{"JSON field idx OOB", &Program{
			Code:  []Instr{{Op: OpJSONFnField, A: 0}, {Op: OpIsNull}},
			JSONs: []JSONOp{{Path: "$.a", FieldIdx: 5}},
		}, "JSON field index 5 out of range"},
		{"unknown builtin id", &Program{
			Code: []Instr{{Op: OpCallB, A: int32(1<<20)<<8 | 1}},
		}, "unknown builtin id"},
		{"builtin arity mismatch", &Program{
			Code: []Instr{{Op: OpCallB, A: 0<<8 | 200}}, // id 0 with 200 args
		}, "argument"},
		{"bad nested sub-program", &Program{
			Code: []Instr{{Op: OpExistsSub, A: 0}},
			Subs: []SubProg{{Coll: "c", Where: &Program{
				Code: []Instr{{Op: OpLoadField, A: 7}, {Op: OpIsNull}},
			}}},
		}, "field index 7 out of range"},
	}
	for _, c := range cases {
		err := c.prog.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Validate err = %v, want substring %q", c.name, err, c.want)
		}
		// Eval must refuse the same program (lazily) — false, never a panic.
		if got := c.prog.Eval(row); got {
			t.Errorf("%s: Eval = true, want false", c.name)
		}
	}

	// Unbounded hand-built Where chains: Eval recurses per nesting level, so
	// Validate bounds them (iteratively — validating hostile input must not
	// itself overflow the stack).
	leaf, err := CompileString("amount > 0")
	if err != nil {
		t.Fatalf("compile leaf: %v", err)
	}
	deep := leaf
	for i := 0; i < 100_000; i++ {
		deep = &Program{
			Code: []Instr{{Op: OpExistsSub, A: 0}},
			Subs: []SubProg{{Coll: "orders", Where: deep}},
		}
	}
	if err := deep.Validate(); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("Validate(100k-deep Where chain) err = %v, want depth error", err)
	}
	if got := deep.Eval(row); got {
		t.Fatalf("Eval(100k-deep Where chain) = true, want false")
	}

	// A hand-built VALID program works: lazily checked on Eval, and Validate
	// marks it for the fast path.
	okProg := &Program{
		Code:   []Instr{{Op: OpLoadField, A: 0}, {Op: OpConstNum, A: 0}, {Op: OpEq}},
		Fields: []string{"x"},
		Nums:   []float64{1},
	}
	if got := okProg.Eval(row); !got {
		t.Fatalf("hand-built valid program Eval = false, want true")
	}
	if err := okProg.Validate(); err != nil {
		t.Fatalf("hand-built valid program Validate = %v, want nil", err)
	}

	// Compiled programs are pre-validated (fast path) and self-checked.
	prog, err := CompileString("x = 1")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !prog.ok {
		t.Fatalf("compiled program not marked valid")
	}
	if err := prog.Validate(); err != nil {
		t.Fatalf("compiled program Validate = %v", err)
	}
}
