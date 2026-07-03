// complex_examples_test.go — proves the per-DSL COMPLEX rule files under
// examples/rules/complex/ are actually parseable+compilable by the matching
// front-end (the "引擎能解析" guarantee), mirroring examples_test.go. Each file
// is loaded via LoadRulesAuto (extension dispatch) and every rule is compiled;
// zero failures are required. It also cross-checks that every rule matches at
// least zero users without runtime panics by evaluating the bundled users file.
//
// Run: go test ./pkg/engine/ -run TestComplexRuleFiles -v
package engine_test

import (
	"path/filepath"
	"testing"

	"tcg-rulex-engine/pkg/engine"
)

func TestComplexRuleFiles(t *testing.T) {
	cases := []struct {
		file     string
		frontend engine.Frontend
		minRules int
	}{
		{"native.sql", engine.NativeFrontend{}, 68},
		{"qlbridge.sql", engine.QLBridgeFrontend{}, 23},
		{"json_rule.json", engine.JSONFrontend{}, 16},
		{"cel.cel", engine.CELFrontend{}, 21},
		{"expr.expr", engine.ExprFrontend{}, 23},
	}
	users, err := engine.LoadUsers(filepath.Join("..", "..", "examples", "rules", "complex", "users.json"))
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "examples", "rules", "complex", c.file)
			rules, err := engine.LoadRulesAuto(path)
			if err != nil {
				t.Fatalf("load %s: %v", c.file, err)
			}
			if len(rules) < c.minRules {
				t.Fatalf("%s: got %d rules, want >= %d", c.file, len(rules), c.minRules)
			}
			eng := engine.NewWithBackend(engine.NewBytecodeBackend(c.frontend))
			loaded, failed := eng.LoadRules(rules)
			if failed != 0 {
				t.Errorf("%s via %s: %d rule(s) failed to compile (want 0)", c.file, c.frontend.Name(), failed)
			}
			if loaded != len(rules) {
				t.Errorf("%s via %s: loaded %d of %d rules", c.file, c.frontend.Name(), loaded, len(rules))
			}
			// Smoke-eval every user against the full rule set (no panics, engine
			// returns a well-formed result slice).
			for _, u := range users {
				_ = eng.Match(u)
			}
		})
	}
}
