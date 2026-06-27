// Command rule-engine-demo loads SQL-WHERE style rules (as if stored in a DB
// `rule_engine` column) and, in real time, decides which rules each incoming
// user wide-table record satisfies — using qlbridge as the evaluation backend.
//
//	go run ./cmd/rule-engine-demo
//	go run ./cmd/rule-engine-demo -rules data/rules.json -workers 8
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"

	"github.com/example/rule-engine-demo/engine"
	qlparser "github.com/example/rule-engine-demo/parser/qlbridge"
	qlruntime "github.com/example/rule-engine-demo/runtime/qlbridge"
)

func main() {
	rulesPath := flag.String("rules", "data/rules.json", "path to rules JSON file")
	workers := flag.Int("workers", 0, "batch worker count (0 = num CPUs)")
	flag.Parse()

	// 1. Build the engine with the qlbridge parser + runtime backend.
	//    Swapping these two lines for a CEL/Expr backend would change the DSL
	//    without touching the engine or callers.
	eng := engine.New(qlparser.New(), qlruntime.New())

	// 2. Load + compile rules once (hot path then only evaluates).
	if err := eng.LoadRulesFromFile(*rulesPath); err != nil {
		log.Fatalf("load rules: %v", err)
	}
	fmt.Printf("✅ loaded %d rules from %s\n\n", eng.RuleCount(), *rulesPath)

	// 3. A batch of users arrives, each with its wide-table row.
	users := sampleUsers()

	// 4. Match the whole batch concurrently (real-time scoring).
	results := eng.MatchBatch(users, *workers)

	// 5. Report.
	printReport(users, results)
}

// sampleUsers returns wide-table records (column -> value) mirroring the
// user_profile table. Keys must match the identifiers used in the rules.
func sampleUsers() []engine.BatchUser {
	return []engine.BatchUser{
		{UserID: 100001, Data: map[string]any{
			"username": "user001", "gender": "男", "age": 28, "province": "广东", "city": "深圳",
			"vip_level": 3, "login_days_30d": 28, "order_count": 132, "total_amount": 35628.56,
			"active_score": 91.52, "credit_score": 765, "risk_level": "低", "device_type": "iPhone",
			"income_level": "20k-30k", "favorite_category": "数码",
		}},
		{UserID: 100002, Data: map[string]any{
			"username": "user002", "gender": "女", "age": 35, "province": "上海", "city": "上海",
			"vip_level": 2, "login_days_30d": 22, "order_count": 58, "total_amount": 12689.00,
			"active_score": 76.30, "credit_score": 698, "risk_level": "中", "device_type": "Android",
			"income_level": "10k-20k", "favorite_category": "图书",
		}},
		{UserID: 100003, Data: map[string]any{
			"username": "user003", "gender": "男", "age": 41, "province": "北京", "city": "北京",
			"vip_level": 4, "login_days_30d": 30, "order_count": 352, "total_amount": 185962.20,
			"active_score": 98.15, "credit_score": 822, "risk_level": "低", "device_type": "Windows",
			"income_level": "30k+", "favorite_category": "汽车",
		}},
		{UserID: 100004, Data: map[string]any{
			"username": "user004", "gender": "女", "age": 23, "province": "浙江", "city": "杭州",
			"vip_level": 1, "login_days_30d": 18, "order_count": 12, "total_amount": 1328.90,
			"active_score": 65.42, "credit_score": 640, "risk_level": "低", "device_type": "iPhone",
			"income_level": "<5k", "favorite_category": "美妆",
		}},
		{UserID: 100005, Data: map[string]any{
			"username": "user005", "gender": "男", "age": 31, "province": "四川", "city": "成都",
			"vip_level": 3, "login_days_30d": 29, "order_count": 168, "total_amount": 60281.80,
			"active_score": 89.73, "credit_score": 781, "risk_level": "低", "device_type": "Mac",
			"income_level": "20k-30k", "favorite_category": "家电",
		}},
		// synthetic high-risk user to trigger the 风控 rule (#3)
		{UserID: 100006, Data: map[string]any{
			"username": "user006", "gender": "男", "age": 48, "province": "广东", "city": "广州",
			"vip_level": 2, "login_days_30d": 3, "order_count": 9, "total_amount": 60000.00,
			"active_score": 40.10, "credit_score": 580, "risk_level": "高", "device_type": "Android",
			"income_level": "10k-20k", "favorite_category": "数码周边",
		}},
	}
}

func printReport(users []engine.BatchUser, results []engine.UserMatches) {
	fmt.Println("=== 实时规则匹配结果 (batch) ===")
	for i, r := range results {
		name, _ := users[i].Data["username"].(string)
		fmt.Printf("\n用户 %d (%s):\n", r.UserID, name)
		if len(r.Matches) == 0 {
			fmt.Println("  ✗ 未命中任何规则")
			continue
		}
		// already priority-sorted, but keep deterministic by id too
		sort.SliceStable(r.Matches, func(a, b int) bool {
			if r.Matches[a].Priority != r.Matches[b].Priority {
				return r.Matches[a].Priority > r.Matches[b].Priority
			}
			return r.Matches[a].RuleID < r.Matches[b].RuleID
		})
		for _, m := range r.Matches {
			fmt.Printf("  ✓ 命中规则 #%d  [%s]  (priority %d)\n", m.RuleID, m.RuleName, m.Priority)
		}
	}
}
