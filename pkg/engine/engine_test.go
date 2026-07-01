// engine_test.go — 引擎装配/加载/匹配单元测试（共享 namedRules / sampleUsers 夹具）。
//
// 运行 / Run:  go test ./pkg/engine/ -v
// 用例 / Cases: TestLoadAndMatch(禁用规则不加载 + 固定命中答案)、TestFrontendsAgree
//   (qlbridge 前端与原生前端命中一致)、TestBatchEqualsSingle(批量=逐个 + 统计校验)、
//   TestMatcherInterface(默认引擎经 Matcher 接口加载/计数)。

package engine_test

import (
	"slices"
	"testing"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

func namedRules() []model.Rule {
	return []model.Rule{
		{
			ID: 1, Name: "广东高价值男性", Priority: 10, Enabled: true,
			Expr: "gender = '男' AND age BETWEEN 25 AND 35 AND province = '广东' AND vip_level >= 3 AND total_amount >= 10000",
		},
		{
			ID: 2, Name: "活跃iPhone低风险", Priority: 8, Enabled: true,
			Expr: "login_days_30d >= 20 AND active_score >= 80 AND order_count >= 10 AND risk_level = '低' AND device_type = 'iPhone'",
		},
		{
			ID: 3, Name: "风控", Priority: 20, Enabled: true,
			Expr: "credit_score < 600 AND total_amount > 50000 AND login_days_30d < 5 AND province IN ('广东','浙江') AND age > 45",
		},
		{
			ID: 4, Name: "数码高活跃", Priority: 12, Enabled: true,
			Expr: "age BETWEEN 25 AND 40 AND province IN ('广东','江苏','浙江') AND income_level IN ('20k-30k','30k+') AND favorite_category LIKE '数%' AND active_score >= 85",
		},
		{
			ID: 5, Name: "女性会员复购", Priority: 6, Enabled: true,
			Expr: "gender = '女' AND age >= 30 AND vip_level >= 2 AND total_amount > 5000 AND login_days_30d >= 15",
		},
		{ID: 6, Name: "已禁用", Priority: 1, Enabled: false, Expr: "age >= 0"},
	}
}

func mkUser(uid int64, f map[string]any) model.User {
	f["uid"] = uid
	return model.User{UID: uid, Fields: f}
}

func sampleUsers() []model.User {
	return []model.User{
		mkUser(1, map[string]any{
			"gender": "男", "age": 28, "province": "广东", "vip_level": 3,
			"login_days_30d": 28, "order_count": 132, "total_amount": 35628.56, "active_score": 91.52,
			"credit_score": 765, "risk_level": "低", "device_type": "iPhone", "income_level": "20k-30k",
			"favorite_category": "数码",
		}),
		mkUser(2, map[string]any{
			"gender": "女", "age": 35, "province": "上海", "vip_level": 2,
			"login_days_30d": 22, "order_count": 58, "total_amount": 12689.00, "active_score": 76.30,
			"credit_score": 698, "risk_level": "中", "device_type": "Android", "income_level": "10k-20k",
			"favorite_category": "图书",
		}),
		mkUser(6, map[string]any{
			"gender": "男", "age": 48, "province": "广东", "vip_level": 2,
			"login_days_30d": 3, "order_count": 9, "total_amount": 60000.00, "active_score": 40.10,
			"credit_score": 580, "risk_level": "高", "device_type": "Android", "income_level": "10k-20k",
			"favorite_category": "数码周边",
		}),
	}
}

// engineWith builds an engine using the bytecode VM with a specific front-end.
func engineWith(fe engine.Frontend) *engine.Engine {
	return engine.NewWithBackend(engine.NewBytecodeBackend(fe))
}

func sortedMatch(eng *engine.Engine, u model.User) []int64 {
	got := eng.Match(u)
	slices.Sort(got)
	return got
}

// TestLoadAndMatch pins known answers using the native front-end + VM.
func TestLoadAndMatch(t *testing.T) {
	eng := engineWith(engine.NativeFrontend{})
	loaded, failed := eng.LoadRules(namedRules())
	if failed != 0 || loaded != 5 { // rule 6 disabled
		t.Fatalf("loaded=%d failed=%d", loaded, failed)
	}

	want := map[int64][]int64{1: {1, 2, 4}, 2: {5}, 6: {3}}
	for _, u := range sampleUsers() {
		got := sortedMatch(eng, u)
		if !equalIDs(got, want[u.UID]) {
			t.Errorf("uid %d: got %v want %v", u.UID, got, want[u.UID])
		}
	}
}

// TestFrontendsAgree verifies the qlbridge front-end (qlbridge AST → IR) produces
// exactly the same matches as the native front-end — i.e. the AST→IR converter
// is correct and the VM is parser-agnostic.
func TestFrontendsAgree(t *testing.T) {
	native := engineWith(engine.NativeFrontend{})
	ql := engineWith(engine.QLBridgeFrontend{})
	if _, f := native.LoadRules(namedRules()); f != 0 {
		t.Fatalf("native failed=%d", f)
	}
	if _, f := ql.LoadRules(namedRules()); f != 0 {
		t.Fatalf("qlbridge failed=%d", f)
	}
	for _, u := range sampleUsers() {
		a := sortedMatch(native, u)
		b := sortedMatch(ql, u)
		if !equalIDs(a, b) {
			t.Errorf("uid %d: native %v != qlbridge %v", u.UID, a, b)
		}
	}
}

func TestBatchEqualsSingle(t *testing.T) {
	eng := engineWith(engine.NativeFrontend{})
	eng.LoadRules(namedRules())
	users := sampleUsers()

	results, stats := eng.RunBatch(users, 4)
	if stats.Users != 3 || stats.Rules != 5 || stats.Evaluations != 15 {
		t.Fatalf("stats users=%d rules=%d evals=%d", stats.Users, stats.Rules, stats.Evaluations)
	}
	byUID := map[int64]model.User{}
	for _, u := range users {
		byUID[u.UID] = u
	}
	for _, r := range results {
		single := sortedMatch(eng, byUID[r.UID])
		batch := append([]int64(nil), r.RuleIDs...)
		slices.Sort(batch)
		if !equalIDs(single, batch) {
			t.Errorf("uid %d: single %v != batch %v", r.UID, single, batch)
		}
	}
}

func TestMatcherInterface(t *testing.T) {
	var m engine.Matcher = engine.New() // default: bytecode + qlbridge frontend
	m.LoadRules(namedRules())
	if m.RuleCount() != 5 {
		t.Fatalf("rule count %d", m.RuleCount())
	}
}

// advancedRules use SQL surface qlbridge cannot parse/convert on its own
// (functions, IS NULL, NOT, REGEXP, JSON_EXTRACT, array predicates, date
// functions). They must compile and match identically under both frontends,
// proving the qlbridge frontend transparently falls back to the native parser.
func advancedRules() []model.Rule {
	return []model.Rule{
		{ID: 101, Name: "lower", Enabled: true, Expr: "LOWER(city) = 'bj'"},
		{ID: 102, Name: "isnotnull", Enabled: true, Expr: "phone IS NOT NULL"},
		{ID: 103, Name: "not", Enabled: true, Expr: "NOT (risk_level = '高')"},
		{ID: 104, Name: "regexp", Enabled: true, Expr: "phone REGEXP '^139'"},
		{ID: 105, Name: "array", Enabled: true, Expr: "ARRAY_CONTAINS(tags, 'vip')"},
		{ID: 106, Name: "json", Enabled: true, Expr: "JSON_EXTRACT(profile, '$.city') = '深圳'"},
		{ID: 107, Name: "datediff", Enabled: true, Expr: "DATEDIFF('2026-06-28', last_login) <= 30"},
		{ID: 108, Name: "datebetween", Enabled: true, Expr: "reg_date BETWEEN '2020-01-01' AND '2020-12-31'"},
		{ID: 109, Name: "roundneg", Enabled: true, Expr: "ROUND(amount, -1) = 120"},
	}
}

// TestQLBridgeFallbackToNative verifies engine.New()'s default qlbridge frontend
// compiles the full documented feature set (via native fallback) and matches
// exactly what the native frontend produces.
func TestQLBridgeFallbackToNative(t *testing.T) {
	native := engineWith(engine.NativeFrontend{})
	ql := engineWith(engine.QLBridgeFrontend{})
	if l, f := native.LoadRules(advancedRules()); f != 0 || l != 9 {
		t.Fatalf("native advanced: loaded=%d failed=%d (want 9,0)", l, f)
	}
	if l, f := ql.LoadRules(advancedRules()); f != 0 || l != 9 {
		t.Fatalf("qlbridge advanced: loaded=%d failed=%d (want 9,0) — fallback not working", l, f)
	}
	u := mkUser(1, map[string]any{
		"city": "BJ", "phone": "13912345678", "risk_level": "低",
		"tags":     []string{"vip", "new"},
		"profile":  `{"city":"深圳"}`,
		"last_login": "2026-06-10", "reg_date": "2020-06-15", "amount": 123.4,
	})
	a := sortedMatch(native, u)
	b := sortedMatch(ql, u)
	if !equalIDs(a, b) {
		t.Fatalf("frontends disagree on advanced rules: native %v != qlbridge %v", a, b)
	}
	// all nine rules are crafted to match this user
	want := []int64{101, 102, 103, 104, 105, 106, 107, 108, 109}
	if !equalIDs(a, want) {
		t.Errorf("advanced matches: got %v want %v", a, want)
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
