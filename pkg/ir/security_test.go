// security_test.go — parser hardening (round seven): the nesting bound that
// protects every recursive consumer of the IR (parser itself, vm.Compile, the
// AST runtime, Emit, Optimize) from stack exhaustion on hostile rule text.
package ir

import (
	"strings"
	"testing"
)

func TestSecurityParseDepthLimit(t *testing.T) {
	// Beyond the bound: a clean load-time error, not a stack overflow.
	deep := strings.Repeat("(", 250) + "x = 1" + strings.Repeat(")", 250)
	if _, err := Parse(deep); err == nil {
		t.Error("expected depth error for 250 nested parens")
	} else if !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("unexpected error: %v", err)
	}

	// NOT chains recurse through the same guard.
	nots := strings.Repeat("NOT ", 250) + "x = 1"
	if _, err := Parse(nots); err == nil {
		t.Error("expected depth error for 250 chained NOTs")
	}

	// Megabyte-of-parens hostile input: must error fast, not crash.
	hostile := strings.Repeat("(", 1<<20)
	if _, err := Parse(hostile); err == nil {
		t.Error("expected error for 1MiB of '('")
	}

	// Well under the bound everything still parses, including sub-query WHERE
	// recursion and quantifiers.
	ok := []string{
		strings.Repeat("(", 150) + "x = 1" + strings.Repeat(")", 150),
		strings.Repeat("NOT ", 150) + "x = 1",
		"EXISTS (SELECT 1 FROM orders WHERE (a = 1 AND (b = 2 OR (c = 3))))",
	}
	for _, rule := range ok {
		if _, err := Parse(rule); err != nil {
			t.Errorf("parse %.40q...: %v", rule, err)
		}
	}

	// Flat breadth is unaffected by the depth bound: 5000 OR-terms parse fine
	// (the parser folds same-level AND/OR iteratively).
	terms := make([]string, 5000)
	for i := range terms {
		terms[i] = "x = 1"
	}
	if _, err := Parse(strings.Join(terms, " OR ")); err != nil {
		t.Errorf("flat 5000-term OR: %v", err)
	}

	// Nested CALLS recurse through parseTerm (not parseUnary) and share the
	// same budget: LOWER(LOWER(...)) 250 deep errors, 150 deep parses.
	if _, err := Parse(callNest(250)); err == nil {
		t.Error("expected depth error for 250 nested calls")
	} else if !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, err := Parse(callNest(150)); err != nil {
		t.Errorf("150 nested calls: %v", err)
	}
}

// callNest builds LOWER(LOWER(...LOWER(x)...)) = 'y' with n call levels.
func callNest(n int) string {
	return strings.Repeat("LOWER(", n) + "x" + strings.Repeat(")", n) + " = 'y'"
}
