// array_scalar_test.go — round thirteen: non-scalar operands (arrays, JSON
// objects, typed nested collections) in SCALAR predicate contexts, plus the
// bare-field BETWEEN term-bound extension and the SPLIT amplification cap.
//
// Historical behaviour being fixed here:
//
//   - kArr rendered as "" in the VM's compare/IN/LIKE/REGEXP fallbacks, so
//     `tags = other_tags` was true for ANY two array fields on the bytecode VM
//     while the AST runtime (termVal nils arrays) said false — a live
//     divergence the differential fuzzer could not see (its scalar-comparison
//     templates never drew array fields).
//   - A pre-parsed JSON object (map[string]any) or a typed []map[string]any
//     collection loaded as kUndef, making `profile IS NULL` TRUE on the VM
//     (false on the AST runtime) — so `profile IS NOT NULL AND
//     JSON_EXTRACT(profile, '$.city') = '深圳'` could never fire on decoded
//     rows while working on JSON-string rows.
//
// The unified semantics (both runtimes, asserted per case below): a non-scalar
// operand behaves like NULL in every comparison / IN / LIKE / REGEXP / scalar
// function, but IS a present value for IS [NOT] NULL. Array/object content is
// reached through ARRAY_*, quantifiers, sub-queries and the JSON operators.
package vm

import (
	"strings"
	"testing"
	"time"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

// evalBoth compiles and evaluates rule on both runtimes, requiring agreement.
func evalBoth(t *testing.T, rule string, row map[string]any) bool {
	t.Helper()
	node, err := ir.Parse(rule)
	if err != nil {
		t.Fatalf("parse %q: %v", rule, err)
	}
	return evalBothNode(t, rule, node, row)
}

func evalBothNode(t *testing.T, label string, node ir.Node, row map[string]any) bool {
	t.Helper()
	prog, err := Compile(node)
	if err != nil {
		t.Fatalf("vm compile %q: %v", label, err)
	}
	bc := prog.Eval(row)
	av, err := astrt.New().Execute(node, row)
	if err != nil {
		t.Fatalf("ast execute %q: %v", label, err)
	}
	if bc != av {
		t.Fatalf("RUNTIME DIVERGENCE on %q: vm=%v ast=%v", label, bc, av)
	}
	return bc
}

func TestArrayScalarPredicates(t *testing.T) {
	row := map[string]any{
		"name":  "",
		"csv":   "a,b,vip",
		"tags":  []any{"vip", "a"},
		"nums":  []any{1, 2, 3},
		"typed": []string{"x", "y"},
	}
	cases := []struct {
		rule string
		want bool
	}{
		// arrays on either side of every comparison shape -> false
		{"tags = nums", false},
		{"tags != nums", false},
		{"tags <= nums", false},
		{"tags = tags", false},   // even the same field: arrays never compare
		{"name = tags", false},   // empty string vs array (VM used to say true)
		{"tags = ''", false},     // legacy Compare path (both used to say true)
		{"tags != ''", false},    // != on a non-scalar is false, like NULL
		{"typed = 'x'", false},   // []string-typed field
		{"UPPER(name) = tags", false},
		{"SPLIT(csv, ',') = ''", false},        // computed array, term path
		{"SPLIT(csv, ',') != ''", false},
		{"tags = ANY (nums)", false},           // array-left quantifier
		{"tags = ALL (nums)", false},           // ALL: non-empty, no match
		// membership / patterns / regexp -> arrays never match
		{"tags IN ('')", false},
		{"tags NOT IN ('')", true}, // two-value NOT, like NOT IN on NULL
		{"tags IN ('vip')", false}, // element text is NOT the array's text
		{"tags LIKE '%'", false},
		{"tags NOT LIKE '%'", true},
		{"tags REGEXP '^'", false}, // '^' matches "" — used to hit on the VM
		{"tags NOT REGEXP '^'", true},
		{"SPLIT(csv, ',') LIKE '%'", false},
		{"SPLIT(csv, ',') REGEXP '^'", false},
		// IS NULL: arrays are PRESENT values
		{"tags IS NULL", false},
		{"tags IS NOT NULL", true},
		{"typed IS NOT NULL", true},
		// scalar functions of arrays are NULL
		{"UPPER(tags) IS NULL", true},
		{"TRIM(tags) IS NULL", true},
		// array semantics still live in the ARRAY_* predicates / quantifiers
		{"ARRAY_CONTAINS(tags, 'vip')", true},
		{"ARRAY_LENGTH(tags) = 2", true},
		{"ARRAY_INTERSECT(tags, nums)", false},
		{"'vip' = ANY (tags)", true},
		{"'b' = ANY (SPLIT(csv, ','))", true},
		{"LENGTH(tags) = 2", true}, // element count, unchanged
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

func TestOpaqueObjectPredicates(t *testing.T) {
	decoded := map[string]any{ // pre-parsed JSON object + typed collection
		"profile": map[string]any{"city": "深圳", "vip": true},
		"orders": []map[string]any{
			{"amount": 100.0, "status": "已付"},
			{"amount": 50.0, "status": "退款"},
		},
		"weird": time.Time{}, // exotic Go type: NOT representable -> NULL
	}
	asString := map[string]any{ // same document as a JSON string
		"profile": `{"city":"深圳","vip":true}`,
	}
	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// presence: a decoded object is a value, exactly like its string form
		{"profile IS NULL", decoded, false}, // VM used to say true here
		{"profile IS NOT NULL", decoded, true},
		{"profile IS NOT NULL", asString, true},
		{"orders IS NOT NULL", decoded, true}, // typed []map collection
		// ...but never matches a scalar predicate
		{"profile = ''", decoded, false}, // AST used to say true here
		{"profile != ''", decoded, false},
		{"profile IN ('')", decoded, false}, // AST used to say true here
		{"profile LIKE '%'", decoded, false},
		{"profile REGEXP '^'", decoded, false},
		{"UPPER(profile) IS NULL", decoded, true},
		// the feature-6 footgun, fixed: guard + JSON access in one rule
		{"profile IS NOT NULL AND JSON_EXTRACT(profile, '$.city') = '深圳'", decoded, true},
		{"profile IS NOT NULL AND JSON_EXTRACT(profile, '$.city') = '深圳'", asString, true},
		{"JSON_EXTRACT(profile, '$.vip') = 'true'", decoded, true},
		// typed nested collections keep working through sub-queries
		{"EXISTS (SELECT 1 FROM orders WHERE amount > 60)", decoded, true},
		{"(SELECT COUNT(*) FROM orders) = 2", decoded, true},
		{"(SELECT SUM(amount) FROM orders WHERE status = '已付') = 100", decoded, true},
		// exotic types stay NULL on both runtimes
		{"weird IS NULL", decoded, true},
		{"weird IS NOT NULL", decoded, false},
		{"weird = ''", decoded, false},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, c.row); got != c.want {
			t.Errorf("%q on %v = %v, want %v", c.rule, c.row, got, c.want)
		}
	}
}

func TestBetweenTermBounds(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	row := map[string]any{
		"age": 5, "min_age": 1, "max_age": 9,
		"reg_date": today,
		"lo":       "2020-01-01", "hi": "2020-12-31",
		"d": "2020-06-15",
	}
	cases := []struct {
		rule string
		want bool
	}{
		// field bounds (new: used to be a parse error on the bare-field path)
		{"age BETWEEN min_age AND max_age", true},
		{"age BETWEEN max_age AND min_age", false}, // inverted range
		{"age BETWEEN min_age AND missing", false}, // NULL bound never matches
		{"d BETWEEN lo AND hi", true},              // date-string field bounds
		// function bounds
		{"reg_date BETWEEN DATE_SUB(CURRENT_DATE, 7) AND CURRENT_DATE", true},
		{"reg_date BETWEEN DATE_ADD(CURRENT_DATE, 1) AND DATE_ADD(CURRENT_DATE, 7)", false},
		// mixed literal + term bound
		{"age BETWEEN 1 AND max_age", true},
		{"age BETWEEN min_age AND 4", false},
		// literal bounds keep their existing fast paths / desugars
		{"age BETWEEN -5 AND 5", true},
		{"age BETWEEN 6 AND 9", false},
		{"d BETWEEN '2020-01-01' AND '2020-12-31'", true},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
		// emit -> reparse -> both runtimes agree with the direct evaluation
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		sql2 := ir.Emit(node, ir.SQL)
		if got := evalBoth(t, sql2, row); got != c.want {
			t.Errorf("round trip %q -> %q = %v, want %v", c.rule, sql2, got, c.want)
		}
	}
}

func TestSplitElementCap(t *testing.T) {
	row := map[string]any{
		"small": "a,b,c",
		"huge":  strings.Repeat(",", 1<<16), // 65537 elements after split
	}
	cases := []struct {
		rule string
		want bool
	}{
		{"ARRAY_LENGTH(SPLIT(small, ',')) = 3", true},
		{"ARRAY_LENGTH(SPLIT(huge, ',')) IS NULL", true}, // capped -> NULL
		{"SPLIT(huge, ',') IS NULL", true},
		{"'a' = ANY (SPLIT(huge, ','))", false}, // NULL array quantifies false
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

// TestExternalIRNonScalar pins the semantics of IR shapes the parser never
// produces (library users constructing ir nodes directly).
func TestExternalIRNonScalar(t *testing.T) {
	row := map[string]any{"tags": []any{"a"}, "n": 5}

	// A string-bounded Between node: the bytecode VM refuses to compile it
	// (strings are not numbers in the value model) and the AST runtime must
	// not quietly evaluate it as numeric — it is false, like every failed
	// numeric coercion.
	strBetween := ir.Between{
		Field: "n",
		Lo:    ir.Value{IsString: true, Str: "1"},
		Hi:    ir.Value{IsString: true, Str: "9"},
	}
	if _, err := Compile(strBetween); err == nil {
		t.Errorf("vm.Compile(string-bounded Between) should fail, got nil error")
	}
	if got, err := astrt.New().Execute(strBetween, row); err != nil || got {
		t.Errorf("ast Execute(string-bounded Between) = (%v, %v), want (false, nil)", got, err)
	}

	// IS NULL over a field term (parser emits ir.IsNull for bare fields, so
	// this shape is external-IR only): an array field is a PRESENT value on
	// both runtimes.
	isNull := ir.IsNullTerm{Left: ir.FieldTerm{Name: "tags"}}
	if got := evalBothNode(t, "IsNullTerm(tags)", isNull, row); got {
		t.Errorf("IsNullTerm over an array field = true, want false (present)")
	}
}
