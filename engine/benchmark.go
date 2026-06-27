package engine

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"strings"
	"time"

	ds "github.com/example/rule-engine-demo/datasource"
	"github.com/example/rule-engine-demo/ir"
	"github.com/example/rule-engine-demo/model"
)

// Config controls a benchmark/match run.
type Config struct {
	RulesFile string // load rules from this JSON file (empty = generate)
	UsersFile string // load users from this JSON file (empty = generate)
	GenRules  int    // number of rules to generate when RulesFile is empty
	GenUsers  int    // number of users to generate when UsersFile is empty
	Workers   int    // worker goroutines (0 = GOMAXPROCS)
	Seed      int64  // RNG seed for reproducible generation
	TopN      int    // print this many most-hit rules (0 = none)
	Demo      bool   // run the named 5-rule demo on sample users and exit
	Serve     string // if set (e.g. ":8080"), start the HTTP scoring server
	Export    string // if set (sql|aviator|cel|expr), convert rules and exit
	Backend   string // "bytecode" (custom VM) or "qlbridge" (qlbridge VM)
	Frontend  string // bytecode parser front-end: "qlbridge" or "native"
	Watch     bool   // with -serve + -rules: hot-reload the rules file on change
}

// DefaultConfig returns the out-of-the-box configuration used by `go run .`.
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
func newEngine(cfg Config) *Engine {
	if cfg.Backend == "qlbridge" {
		return NewWithBackend(NewQLBridgeBackend())
	}
	var fe Frontend
	switch cfg.Frontend {
	case "native":
		fe = NativeFrontend{}
	case "json":
		fe = JSONFrontend{}
	case "cel":
		fe = CELFrontend{}
	case "expr":
		fe = ExprFrontend{}
	default:
		fe = QLBridgeFrontend{}
	}
	return NewWithBackend(NewBytecodeBackend(fe))
}

// RunCLI parses command-line flags and runs the engine. This is the single
// entry point shared by ./main.go and ./cmd/main.go.
func RunCLI() {
	cfg := DefaultConfig()
	flag.StringVar(&cfg.RulesFile, "rules", "", "rules JSON file (default: generate)")
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

// RunServer loads rules (from file or generated) and serves the HTTP API.
func RunServer(cfg Config) error {
	eng := newEngine(cfg)
	gen := ds.NewGenerator(cfg.Seed)
	rules, err := loadOrGenRules(cfg, gen)
	if err != nil {
		return err
	}
	loaded, failed := eng.LoadRules(rules)
	fmt.Printf("Backend=%s  Rule Cache Ready. (compiled=%d failed=%d)\n",
		eng.Backend().Name(), loaded, failed)

	if cfg.Watch && cfg.RulesFile != "" {
		w := NewWatcher(eng, cfg.RulesFile, 2*time.Second)
		go func() { _ = w.Run(context.Background()) }()
		fmt.Printf("watching %s for changes (hot reload)\n", cfg.RulesFile)
	}
	return Serve(cfg.Serve, eng)
}

// RunExport loads rules and prints each one converted to the requested DSL
// (sql | aviator | cel | expr), demonstrating SQL → IR → multi-DSL conversion.
func RunExport(cfg Config) error {
	dsl := ir.DSL(strings.ToLower(cfg.Export))
	valid := false
	for _, d := range ir.AllDSLs {
		if d == dsl {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown DSL %q (want sql|aviator|cel|expr)", cfg.Export)
	}

	path := cfg.RulesFile
	if path == "" {
		path = "data/rules.json"
	}
	rules, err := LoadRules(path)
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

// Run executes the full pipeline and prints the required report.
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

func loadOrGenRules(cfg Config, gen *ds.Generator) ([]model.Rule, error) {
	if cfg.RulesFile != "" {
		return LoadRules(cfg.RulesFile)
	}
	return gen.Rules(cfg.GenRules), nil
}

func loadOrGenUsers(cfg Config, gen *ds.Generator) ([]model.User, error) {
	if cfg.UsersFile != "" {
		return LoadUsers(cfg.UsersFile)
	}
	return gen.Users(cfg.GenUsers), nil
}

func printTopRules(eng *Engine, stats *Stats, n int) {
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

// RunDemo loads the named example rules + sample users and prints, for each
// user, which named rules they matched. Demonstrates correctness with readable
// rules rather than raw throughput.
func RunDemo(cfg Config) error {
	eng := newEngine(cfg)
	loaded, failed, err := eng.LoadRulesFromFile("data/rules.json")
	if err != nil {
		return err
	}
	fmt.Printf("Backend=%s  loaded %d rules (failed=%d)\n", eng.Backend().Name(), loaded, failed)

	users, err := LoadUsers("data/users.json")
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
