// Package cli 实现规则引擎命令行工具的全部逻辑，由 cmd/cli 这一薄包装入口调用。
//
// 它是一个 flag 驱动的「多功能工具」，把以下几种用法收敛到单个二进制里：
//
//	演示    -demo               可读的命名规则匹配演示（data/*.json）
//	导出    -export <dsl>       SQL 规则 → sql|aviator|cel|expr
//	起服务  -serve <addr>       完整运维控制台 / 评分 API（含 Swagger UI）
//	压测    (默认)              生成/加载规则+用户，批量匹配并打印性能报告
//
// 调用链：cmd/cli/main.go → cli.RunCLI() → 解析 flag → 分派到 Run* 函数之一。
// 各子命令的完整命令行示例见 RunCLI 的文档。
//
// 与 cmd/api 的关系：cmd/api 是分层的生产 HTTP 服务（只读 TOML 配置，无这些
// flag）；本包通过 -serve 起的服务复用了相同的分层 handler 和 Swagger
// （internal/router.Serve / NewApp / RegisterHandlers），因此两条路径暴露的接口
// 完全一致。
package cli

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"strings"
	"time"

	ds "tcg-rulex-engine/internal/datasource"
	"tcg-rulex-engine/internal/router"
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/ir"
	"tcg-rulex-engine/pkg/model"
)

// Config 汇总了一次 CLI 运行的全部可调参数，逐项与命令行 flag 一一对应
// （映射关系见 RunCLI）。字段刻意按内存对齐排序（string/int64 在前，int、bool
// 在后），与业务可读顺序不同，阅读时以下方注释为准。
type Config struct {
	// Serve 非空时启动 HTTP 服务并监听该地址，例如 ":8080"。对应 -serve。
	Serve string
	// UsersFile 指定用户 JSON 文件；留空则按 GenUsers 随机生成。对应 -users。
	UsersFile string
	// Frontend 是字节码后端的解析前端：qlbridge|native|json|cel|expr。对应 -frontend。
	// 注意：-serve 与 -demo 会强制使用 native 前端（见各自函数说明）。
	Frontend string
	// RulesFile 指定规则 JSON 文件；留空则按 GenRules 随机生成。对应 -rules。
	RulesFile string
	// Backend 是求值后端：bytecode（默认，SQL→IR→VM）或 qlbridge（直接在
	// qlbridge VM 上求值，便于 A/B 对比）。对应 -backend。
	Backend string
	// Export 非空时把规则转换成该 DSL（sql|aviator|cel|expr）后退出。对应 -export。
	Export string
	// GenUsers 是随机生成的用户数（仅当 UsersFile 为空时生效）。对应 -gen-users。
	GenUsers int
	// TopN 控制压测报告中按命中数展示的前 N 条规则（规则数 >50 时）。对应 -top。
	TopN int
	// Seed 是随机数据生成器种子，固定它可复现同一批规则/用户。对应 -seed。
	Seed int64
	// Workers 是批量匹配的并发 goroutine 数。对应 -workers。
	Workers int
	// GenRules 是随机生成的规则数（仅当 RulesFile 为空时生效）。对应 -gen-rules。
	GenRules int
	// Demo 为 true 时运行命名规则演示后退出。对应 -demo。
	Demo bool
	// Watch 为 true 且同时给了 -serve -rules 时，监听规则文件变化并热加载。对应 -watch。
	Watch bool
}

// DefaultConfig 返回开箱即用的默认配置（即不带任何 flag 运行 `go run ./cmd/cli`
// 时的取值）：随机生成 1 万条规则 × 1 万个用户、32 worker、固定种子 42、字节码
// 后端 + qlbridge 前端。RunCLI 会在此基础上用命令行 flag 覆盖各字段。
func DefaultConfig() Config {
	return Config{
		GenRules: 10000,
		GenUsers: 10000,
		Workers:  32,
		Seed:     42,
		TopN:     10,
		Backend:  "bytecode",
		Frontend: "qlbridge",
	}
}

// newEngine builds an engine according to cfg.Backend / cfg.Frontend.
//
//	-backend bytecode  -frontend qlbridge   (default) SQL→qlbridge→IR→VM
//	-backend bytecode  -frontend native               SQL→native→IR→VM
//	-backend qlbridge                                  evaluate on qlbridge VM
func newEngine(cfg Config) *engine.Engine {
	if cfg.Backend == "qlbridge" {
		return engine.NewWithBackend(engine.NewQLBridgeBackend())
	}
	var fe engine.Frontend
	switch cfg.Frontend {
	case "native":
		fe = engine.NativeFrontend{}
	case "json":
		fe = engine.JSONFrontend{}
	case "cel":
		fe = engine.CELFrontend{}
	case "expr":
		fe = engine.ExprFrontend{}
	default:
		fe = engine.QLBridgeFrontend{}
	}
	return engine.NewWithBackend(engine.NewBytecodeBackend(fe))
}

// RunCLI 解析命令行 flag 并运行引擎，是 cmd/cli 的唯一入口。
//
// 它先以 DefaultConfig 为基线注册并解析所有 flag，然后按「互斥子命令」的优先级
// 依次判断：-demo → -export → -serve → 默认压测，命中第一个即执行并返回。
//
// # 支持的 flag
//
//	-rules <file>     规则 JSON 文件（缺省：随机生成 GenRules 条）
//	-users <file>     用户 JSON 文件（缺省：随机生成 GenUsers 个）
//	-gen-rules <n>    随机生成的规则数（默认 10000，仅 -rules 为空时生效）
//	-gen-users <n>    随机生成的用户数（默认 10000，仅 -users 为空时生效）
//	-workers <n>      批量匹配的并发 goroutine 数（默认 32）
//	-seed <n>         随机生成器种子（默认 42，固定以复现）
//	-top <n>          压测报告展示命中最多的前 N 条规则（默认 10）
//	-demo             运行 data/*.json 的命名规则演示后退出
//	-serve <addr>     在该地址启动 HTTP 服务，如 :8080（含 Swagger UI）
//	-export <dsl>     规则转 DSL：sql|aviator|cel|expr，打印后退出
//	-backend <b>      求值后端：bytecode（默认）| qlbridge
//	-frontend <f>     字节码前端：qlbridge（默认）| native | json
//	-watch            配合 -serve -rules：规则文件变更时热加载
//
// # 示例
//
//	go run ./cmd/cli                                  # 默认压测（1万规则×1万用户）
//	go run ./cmd/cli -demo                            # 命名规则演示
//	go run ./cmd/cli -serve :8080                     # 起完整运维服务 + Swagger
//	go run ./cmd/cli -serve :8080 -rules data/rules.json -watch  # 真实规则 + 热加载
//	go run ./cmd/cli -export cel                      # SQL → CEL
//	go run ./cmd/cli -gen-rules 50000 -workers 64     # 自定义规模压测
func RunCLI() {
	cfg := DefaultConfig()
	flag.StringVar(&cfg.RulesFile, "rules", "", "rules file: .json rule-set, or .sql/.cel/.expr text (one rule per line); default: generate")
	flag.StringVar(&cfg.UsersFile, "users", "", "users JSON file (default: generate)")
	flag.IntVar(&cfg.GenRules, "gen-rules", cfg.GenRules, "rules to generate")
	flag.IntVar(&cfg.GenUsers, "gen-users", cfg.GenUsers, "users to generate")
	flag.IntVar(&cfg.Workers, "workers", cfg.Workers, "worker goroutines")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "generator seed")
	flag.IntVar(&cfg.TopN, "top", cfg.TopN, "show N most-hit rules")
	flag.BoolVar(&cfg.Demo, "demo", false, "run named 5-rule demo (data/*.json) and exit")
	flag.StringVar(&cfg.Serve, "serve", "", "serve HTTP scoring API on this addr, e.g. :8080")
	flag.StringVar(&cfg.Export, "export", "", "convert rules to DSL (sql|aviator|cel|expr) and exit")
	flag.StringVar(&cfg.Backend, "backend", cfg.Backend, "evaluation backend: bytecode|qlbridge")
	flag.StringVar(&cfg.Frontend, "frontend", cfg.Frontend, "bytecode parser front-end: qlbridge|native|json")
	flag.BoolVar(&cfg.Watch, "watch", false, "with -serve -rules: hot-reload rules file on change")
	flag.Parse()

	if cfg.Demo {
		if err := RunDemo(cfg); err != nil {
			fmt.Println("demo error:", err)
		}
		return
	}
	if cfg.Export != "" {
		if err := RunExport(cfg); err != nil {
			fmt.Println("export error:", err)
		}
		return
	}
	if cfg.Serve != "" {
		if err := RunServer(cfg); err != nil {
			fmt.Println("server error:", err)
		}
		return
	}
	if err := Run(cfg); err != nil {
		fmt.Println("error:", err)
	}
}

// RunServer 加载规则（来自 -rules 文件或随机生成）并启动 HTTP 服务，对应 -serve。
//
// 服务端固定使用「native SQL 前端 + 字节码 VM」，因为实时规则接口
// （/rules、/rules/selftest、/evaluate）需要支持完整算子集，包括 NOT IN /
// NOT LIKE / NOT (...)；目前只有 native 前端能把这些降级成 IR（qlbridge 前端
// 尚不支持，见 pkg/parser/qlbridge）。指定 -backend qlbridge 时则改用 qlbridge VM，
// 仅用于 A/B 对比。
//
// 若同时给了 -watch 和 -rules，会后台启动一个 2 秒轮询的 Watcher 热加载规则文件。
// 实际的路由与 Swagger 挂载在 internal/router.Serve 中完成（与 cmd/api 同一套
// 分层 handler）。
//
// 示例：
//
//	go run ./cmd/cli -serve :8080
//	go run ./cmd/cli -serve :8080 -rules data/rules.json -watch
func RunServer(cfg Config) error {
	// Serving uses the native SQL front-end + bytecode VM so the live rule API
	// (/rules, /rules/selftest, /evaluate) accepts the full operator set,
	// including NOT IN / NOT LIKE / NOT (...). (-backend qlbridge still selects
	// the qlbridge VM for A/B comparison.)
	var eng *engine.Engine
	if cfg.Backend == "qlbridge" {
		eng = engine.NewWithBackend(engine.NewQLBridgeBackend())
	} else {
		eng = engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	}
	gen := ds.NewGenerator(cfg.Seed)
	rules, err := loadOrGenRules(cfg, gen)
	if err != nil {
		return err
	}
	loaded, failed := eng.LoadRules(rules)
	fmt.Printf("Backend=%s  Rule Cache Ready. (compiled=%d failed=%d)\n",
		eng.Backend().Name(), loaded, failed)

	if cfg.Watch && cfg.RulesFile != "" {
		w := engine.NewWatcher(eng, cfg.RulesFile, 2*time.Second)
		go func() { _ = w.Run(context.Background()) }()
		fmt.Printf("watching %s for changes (hot reload)\n", cfg.RulesFile)
	}
	return router.Serve(cfg.Serve, eng)
}

// RunExport 加载规则并把每条规则转换为指定 DSL（sql | aviator | cel | expr）后
// 打印，演示「SQL → IR → 多 DSL」的转换能力，对应 -export。
//
// 规则来源为 -rules 指定的文件，未指定时默认读取 data/rules.json。未知 DSL 会返回
// 错误；单条规则转换失败只打印该行错误并继续，不中断整体输出。
//
// 示例：
//
//	go run ./cmd/cli -export cel
//	go run ./cmd/cli -export aviator -rules data/rules.json
func RunExport(cfg Config) error {
	dsl := ir.DSL(strings.ToLower(cfg.Export))
	valid := false
	for _, d := range ir.AllDSLs {
		if d == dsl {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown DSL %q (want sql|aviator|cel|expr|json)", cfg.Export)
	}

	path := cfg.RulesFile
	if path == "" {
		path = "data/rules.json"
	}
	rules, err := engine.LoadRulesAuto(path)
	if err != nil {
		return err
	}

	fmt.Printf("# converting %d rules from %s to %s\n\n", len(rules), path, dsl)
	for _, r := range rules {
		out, cerr := ir.Convert(r.Expr, dsl)
		fmt.Printf("# rule %d %s\n", r.ID, r.Name)
		fmt.Printf("SQL : %s\n", r.Expr)
		if cerr != nil {
			fmt.Printf("%-4s: <error: %v>\n\n", dsl, cerr)
			continue
		}
		fmt.Printf("%-4s: %s\n\n", dsl, out)
	}
	return nil
}

// Run 是默认子命令（不带 -demo/-export/-serve 时执行），跑完整的离线基准压测流水线：
// 规则「读取/生成 → 解析 → AST → 缓存」，用户「读取/生成」，再以 cfg.Workers 个
// goroutine 批量匹配，最后打印吞吐与延迟报告。规则数 ≤50 时逐条打印命中统计，
// 否则只打印命中最多的前 cfg.TopN 条。后端/前端由 -backend/-frontend 决定。
//
// 示例：
//
//	go run ./cmd/cli                                  # 默认 1万规则 × 1万用户
//	go run ./cmd/cli -gen-rules 50000 -gen-users 100000 -workers 64
//	go run ./cmd/cli -rules data/rules.json -users data/users.json
func Run(cfg Config) error {
	fmt.Printf("CPU=%d GOMAXPROCS=%d\n", runtime.NumCPU(), runtime.GOMAXPROCS(0))

	eng := newEngine(cfg)
	fmt.Printf("Backend=%s\n", eng.Backend().Name())
	gen := ds.NewGenerator(cfg.Seed)

	// ---- Rules: Read → Parse → AST → Cache --------------------------------
	rules, err := loadOrGenRules(cfg, gen)
	if err != nil {
		return err
	}
	fmt.Printf("加载%d条规则...\n", len(rules))
	t0 := time.Now()
	loaded, failed := eng.LoadRules(rules)
	fmt.Printf("Rule Cache Ready. (compiled=%d failed=%d in %s)\n",
		loaded, failed, time.Since(t0).Round(time.Millisecond))

	// ---- Users ------------------------------------------------------------
	users, err := loadOrGenUsers(cfg, gen)
	if err != nil {
		return err
	}
	fmt.Printf("加载%d个用户...\n", len(users))

	// ---- Match ------------------------------------------------------------
	fmt.Println("开始匹配...")
	_, stats := eng.RunBatch(users, cfg.Workers)

	fmt.Print(stats.Report())

	nameOf := func(id int64) string {
		if p, ok := eng.Cache().Get(id); ok {
			return p.Name
		}
		return ""
	}
	if eng.RuleCount() <= 50 {
		// Small rule set: print every rule's hit count for operations.
		fmt.Println("\n=== 命中统计 (Statistics) ===")
		fmt.Print(stats.RuleHits(nameOf))
	} else {
		printTopRules(eng, stats, cfg.TopN)
	}
	return nil
}

// loadOrGenRules 返回规则集：给了 -rules 就从文件加载，否则用 gen 随机生成
// cfg.GenRules 条。
func loadOrGenRules(cfg Config, gen *ds.Generator) ([]model.Rule, error) {
	if cfg.RulesFile != "" {
		// LoadRulesAuto dispatches by extension: .json → rule-set array,
		// otherwise one rule expression per line (SQL/CEL/Expr text files).
		return engine.LoadRulesAuto(cfg.RulesFile)
	}
	return gen.Rules(cfg.GenRules), nil
}

// loadOrGenUsers 返回用户集：给了 -users 就从文件加载，否则用 gen 随机生成
// cfg.GenUsers 个。
func loadOrGenUsers(cfg Config, gen *ds.Generator) ([]model.User, error) {
	if cfg.UsersFile != "" {
		return engine.LoadUsers(cfg.UsersFile)
	}
	return gen.Users(cfg.GenUsers), nil
}

// printTopRules 打印命中数最高的前 n 条规则（n<=0 或无统计时直接返回）。
func printTopRules(eng *engine.Engine, stats *engine.Stats, n int) {
	if n <= 0 || len(stats.PerRule) == 0 {
		return
	}
	fmt.Printf("\nTop %d rules by hits:\n", n)
	for _, rh := range stats.TopRules(n) {
		name := ""
		if p, ok := eng.Cache().Get(rh.ID); ok {
			name = p.Name
		}
		fmt.Printf("  #%d %-12s hits=%s\n", rh.ID, name, comma(rh.Count))
	}
}

// RunDemo 加载 data/rules.json 的命名示例规则与 data/users.json 的样例用户，逐个
// 用户打印其命中的命名规则，对应 -demo。它用「可读的规则」展示正确性，而不是像
// Run 那样追求吞吐。
//
// 其中旗舰规则（data/rules.json 第 6 条）覆盖了完整算子集，包括 NOT IN /
// NOT LIKE / NOT (...)。目前只有 native SQL 前端能把这些降级成 IR，因此本演示
// 无论 -frontend 取值如何都固定使用 native 前端 + 字节码 VM。
//
// 示例：
//
//	go run ./cmd/cli -demo
func RunDemo(cfg Config) error {
	// The flagship rule (#6 in data/rules.json) exercises the full operator set,
	// including NOT IN / NOT LIKE / NOT (...). Only the native SQL front-end
	// lowers those to IR today (see pkg/parser/qlbridge limitations), so the
	// demo pins the native front-end + bytecode VM regardless of -frontend.
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	loaded, failed, err := eng.LoadRulesFromFile("data/rules.json")
	if err != nil {
		return err
	}
	fmt.Printf("Backend=%s  front-end=native-sql  loaded %d rules (failed=%d)\n",
		eng.Backend().Name(), loaded, failed)

	users, err := engine.LoadUsers("data/users.json")
	if err != nil {
		return err
	}

	fmt.Println("\n=== 实时规则匹配结果 ===")
	for _, u := range users {
		ids := eng.Match(u)
		label, _ := u.Fields["username"].(string)
		if label == "" {
			label = fmt.Sprintf("uid=%d", u.UID)
		}
		if len(ids) == 0 {
			fmt.Printf("用户 %s: ✗ 未命中\n", label)
			continue
		}
		fmt.Printf("用户 %s:\n", label)
		for _, id := range ids {
			p, _ := eng.Cache().Get(id)
			name := ""
			if p != nil {
				name = p.Name
			}
			fmt.Printf("  ✓ #%d [%s]\n", id, name)
		}
	}
	return nil
}

// comma formats an integer with thousands separators: 2300000 -> "2,300,000".
func comma(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
