// ir_test.go — IR 解析（SQL→IR）与多 DSL 发射（SQL/CEL/Expr/Aviator）单元测试。
//
// 运行 / Run:  go test ./pkg/ir/ -v
// 用例 / Cases: TestConvertMultiDSL(四 DSL 精确发射)、TestEqualityMapping、TestOrParens、
//   TestLikeVariants、TestIsNull、TestParseErrors(非法表达式报错)、TestNegationConvert
//   (<>/NOT IN/NOT LIKE/NOT(...))、TestFullCoverageRoundTrip(旗舰规则 + SQL 发射幂等)。

package ir

import (
	"strings"
	"testing"
)

func TestConvertMultiDSL(t *testing.T) {
	rule := "age BETWEEN 25 AND 40 AND province IN ('广东','江苏','浙江') AND favorite_category LIKE '数%' AND active_score >= 85"

	cases := map[DSL]string{
		SQL:     "age BETWEEN 25 AND 40 AND province IN ('广东', '江苏', '浙江') AND favorite_category LIKE '数%' AND active_score >= 85",
		CEL:     "(age >= 25 && age <= 40) && province in ['广东', '江苏', '浙江'] && favorite_category.startsWith('数') && active_score >= 85",
		Expr:    "(age >= 25 && age <= 40) && province in [\"广东\", \"江苏\", \"浙江\"] && hasPrefix(favorite_category, \"数\") && active_score >= 85",
		Aviator: "(age >= 25 && age <= 40) && (province == '广东' || province == '江苏' || province == '浙江') && string.startsWith(favorite_category, '数') && active_score >= 85",
	}

	for dsl, want := range cases {
		got, err := Convert(rule, dsl)
		if err != nil {
			t.Fatalf("%s: %v", dsl, err)
		}
		if got != want {
			t.Errorf("%s mismatch:\n got: %s\nwant: %s", dsl, got, want)
		}
	}
}

func TestEqualityMapping(t *testing.T) {
	got, err := Convert("gender = '男' AND vip_level >= 3", CEL)
	if err != nil {
		t.Fatal(err)
	}
	want := "gender == '男' && vip_level >= 3"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestOrParens(t *testing.T) {
	got, err := Convert("(a = 1 OR b = 2) AND c >= 3", Expr)
	if err != nil {
		t.Fatal(err)
	}
	want := "(a == 1 || b == 2) && c >= 3"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestLikeVariants(t *testing.T) {
	cases := []struct {
		rule string
		dsl  DSL
		want string
	}{
		{"name LIKE '%数'", CEL, "name.endsWith('数')"},
		{"name LIKE '%数%'", CEL, "name.contains('数')"},
		{"name LIKE '数'", CEL, "name == '数'"},
		{"name LIKE '数%'", Aviator, "string.startsWith(name, '数')"},
	}
	for _, c := range cases {
		got, err := Convert(c.rule, c.dsl)
		if err != nil {
			t.Fatalf("%s: %v", c.rule, err)
		}
		if got != c.want {
			t.Errorf("%s [%s]: got %q want %q", c.rule, c.dsl, got, c.want)
		}
	}
}

func TestIsNull(t *testing.T) {
	cases := []struct {
		rule string
		dsl  DSL
		want string
	}{
		{"phone IS NULL", SQL, "phone IS NULL"},
		{"phone IS NOT NULL", SQL, "phone IS NOT NULL"},
		{"phone IS NULL", CEL, "!has(phone)"},
		{"phone IS NOT NULL", CEL, "has(phone)"},
		{"phone IS NULL", Expr, "phone == nil"},
		{"phone IS NOT NULL", Aviator, "phone != nil"},
	}
	for _, c := range cases {
		got, err := Convert(c.rule, c.dsl)
		if err != nil {
			t.Fatalf("%s: %v", c.rule, err)
		}
		if got != c.want {
			t.Errorf("%s [%s]: got %q want %q", c.rule, c.dsl, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"age >",            // missing value
		"age BETWEEN 1",    // missing AND hi
		"province IN ()",   // empty list -> needs value
		"age == 1 AND",     // dangling AND
		"@bad",             // bad char
	}
	for _, r := range bad {
		if _, err := Parse(r); err == nil {
			t.Errorf("expected error for %q", r)
		}
	}
}

// flagshipRule exercises every supported operator in one expression.
const flagshipRule = "age BETWEEN 25 AND 40 " +
	"AND province IN ('广东','江苏','浙江') " +
	"AND income_level NOT IN ('<5k','5k-10k') " +
	"AND favorite_category LIKE '数%' " +
	"AND occupation NOT LIKE '%学生%' " +
	"AND active_score >= 85 " +
	"AND credit_score BETWEEN 700 AND 850 " +
	"AND total_amount > 5000 " +
	"AND avg_order_amount <= 1000 " +
	"AND last_login_time IS NOT NULL " +
	"AND (vip_level >= 3 OR order_count >= 30) " +
	"AND NOT (risk_level = '高') " +
	"AND register_days >= 180 " +
	"AND marital_status <> '未知'"

func TestNegationConvert(t *testing.T) {
	cases := []struct {
		rule string
		dsl  DSL
		want string
	}{
		// <> is a SQL alias for != (normalised to != on emit).
		{"marital_status <> '未知'", SQL, "marital_status != '未知'"},
		{"marital_status <> '未知'", CEL, "marital_status != '未知'"},
		// NOT IN
		{"income_level NOT IN ('<5k','5k-10k')", SQL, "income_level NOT IN ('<5k', '5k-10k')"},
		{"income_level NOT IN ('<5k','5k-10k')", CEL, "!(income_level in ['<5k', '5k-10k'])"},
		{"income_level NOT IN ('<5k','5k-10k')", Expr, "!(income_level in [\"<5k\", \"5k-10k\"])"},
		// NOT LIKE
		{"occupation NOT LIKE '%学生%'", SQL, "occupation NOT LIKE '%学生%'"},
		{"occupation NOT LIKE '%学生%'", CEL, "!(occupation.contains('学生'))"},
		{"occupation NOT LIKE '%学生%'", Aviator, "!(string.contains(occupation, '学生'))"},
		// NOT (group)
		{"NOT (risk_level = '高')", SQL, "NOT (risk_level = '高')"},
		{"NOT (risk_level = '高')", CEL, "!(risk_level == '高')"},
		{"NOT (a = 1 OR b = 2)", SQL, "NOT (a = 1 OR b = 2)"},
	}
	for _, c := range cases {
		got, err := Convert(c.rule, c.dsl)
		if err != nil {
			t.Fatalf("%s [%s]: %v", c.rule, c.dsl, err)
		}
		if got != c.want {
			t.Errorf("%s [%s]:\n got %q\nwant %q", c.rule, c.dsl, got, c.want)
		}
	}
}

func TestFullCoverageRoundTrip(t *testing.T) {
	sql, err := Convert(flagshipRule, SQL)
	if err != nil {
		t.Fatalf("parse/emit: %v", err)
	}
	for _, want := range []string{
		"age BETWEEN 25 AND 40",
		"province IN ('广东', '江苏', '浙江')",
		"income_level NOT IN ('<5k', '5k-10k')",
		"favorite_category LIKE '数%'",
		"occupation NOT LIKE '%学生%'",
		"credit_score BETWEEN 700 AND 850",
		"last_login_time IS NOT NULL",
		"(vip_level >= 3 OR order_count >= 30)",
		"NOT (risk_level = '高')",
		"marital_status != '未知'",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL emit missing %q\n full: %s", want, sql)
		}
	}

	// Every target DSL must emit something non-empty without panicking.
	node, err := Parse(flagshipRule)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range AllDSLs {
		if out := Emit(node, d); out == "" {
			t.Errorf("%s: empty emit", d)
		}
	}

	// SQL emit must be parse-idempotent (emit -> parse -> emit is a fixpoint).
	if again, _ := Convert(sql, SQL); again != sql {
		t.Errorf("SQL round-trip not idempotent:\n 1st: %s\n 2nd: %s", sql, again)
	}
}
