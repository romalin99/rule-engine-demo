// null_compare_test.go — regression for the divergence caught by
// TestDifferentialFuzz on its first real run (iteration 102):
//
//	rule "rate <= -75" on an empty row: VM=false, AST=true.
//
// Root cause: the AST runtime's legacy field-vs-literal compare() fell through
// to the string path for non-numeric values INCLUDING nil, and asString(nil)=""
// made `missing <= -75`, `missing != 'a'` and `missing < 'a'` all true. SQL
// semantics (and the bytecode VM's kUndef guard, and compareVals' nil guard)
// say a NULL comparison never matches, for every operator.
//
// This pins missing AND explicit-null fields × all six operators × numeric /
// string / empty-string / negative literals to false on BOTH runtimes, plus
// present-value sanity rows.
//
// Run: go test ./pkg/vm/ -run TestNullCompare -v
package vm

import (
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

func TestNullCompareParity(t *testing.T) {
	missing := map[string]any{}
	explicitNil := map[string]any{"rate": nil}

	ops := []string{"=", "!=", ">", ">=", "<", "<="}
	lits := []string{"-75", "5", "0", "'a'", "''", "'深圳'"}

	rt := astrt.New()
	run := func(rule string, row map[string]any) (bool, bool) {
		node, err := ir.Parse(rule)
		if err != nil {
			t.Fatalf("parse %q: %v", rule, err)
		}
		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("compile %q: %v", rule, err)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("ast compile %q: %v", rule, err)
		}
		a, err := rt.Execute(plan, row)
		if err != nil {
			t.Fatalf("ast execute %q: %v", rule, err)
		}
		return prog.Eval(row), a
	}

	for _, op := range ops {
		for _, lit := range lits {
			rule := "rate " + op + " " + lit
			for name, row := range map[string]map[string]any{"missing": missing, "nil": explicitNil} {
				vm, ast := run(rule, row)
				if vm != ast {
					t.Errorf("%s field: VM/AST disagree on %q: vm=%v ast=%v", name, rule, vm, ast)
				}
				if vm || ast {
					t.Errorf("%s field: %q must not match (SQL NULL semantics), vm=%v ast=%v", name, rule, vm, ast)
				}
			}
		}
	}

	// Two-valued NOT over a NULL comparison stays true on both runtimes
	// (documented engine behaviour, consistent with NOT LIKE / NOT IN on NULL).
	vm, ast := run("NOT (rate <= -75)", missing)
	if vm != ast || !vm {
		t.Errorf("NOT over NULL compare: vm=%v ast=%v, want true/true", vm, ast)
	}

	// Present values still compare normally (guard must not over-reach).
	sane := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"rate <= -75", map[string]any{"rate": -80}, true},
		{"rate <= -75", map[string]any{"rate": 1}, false},
		{"rate != 'a'", map[string]any{"rate": "b"}, true},
		{"rate != 'a'", map[string]any{"rate": "a"}, false},
		{"rate < 'a'", map[string]any{"rate": ""}, true}, // present empty string sorts first
		{"rate = ''", map[string]any{"rate": ""}, true},
		{"rate >= 5", map[string]any{"rate": "not-a-number"}, true}, // mixed types order lexically on both runtimes ('n' > '5')
	}
	for _, c := range sane {
		vm, ast := run(c.rule, c.row)
		if vm != ast {
			t.Errorf("present: VM/AST disagree on %q row=%v: vm=%v ast=%v", c.rule, c.row, vm, ast)
		}
		if vm != c.want {
			t.Errorf("present: %q row=%v got %v want %v", c.rule, c.row, vm, c.want)
		}
	}
}
