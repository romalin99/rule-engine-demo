package ir

import "testing"

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
