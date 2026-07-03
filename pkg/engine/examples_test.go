// examples_test.go — proves the per-DSL example rule files under
// examples/rules/ are actually parseable+compilable by the matching front-end
// (the "引擎能解析" guarantee). Each file is loaded via LoadRulesAuto (extension
// dispatch) and every rule is compiled; zero failures are required.
//
// Run: go test ./pkg/engine/ -run TestExampleRuleFiles -v
package engine_test

import (
	"path/filepath"
	"testing"

	"tcg-rulex-engine/pkg/engine"
)

func TestExampleRuleFiles(t *testing.T) {
	cases := []struct {
		file     string
		frontend engine.Frontend
		minRules int
	}{
		{"native.sql", engine.NativeFrontend{}, 30},
		{"qlbridge.sql", engine.QLBridgeFrontend{}, 16},
		{"json_rule.json", engine.JSONFrontend{}, 12},
		{"cel.cel", engine.CELFrontend{}, 13},
		{"expr.expr", engine.ExprFrontend{}, 13},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "examples", "rules", c.file)
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
		})
	}
}
