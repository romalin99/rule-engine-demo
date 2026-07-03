// loader_lines_test.go — LoadRuleLines / LoadRulesAuto: plain-text rule files
// (one rule per line, '#' comments, optional "name: expr" label).
package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"tcg-rulex-engine/pkg/engine"
)

func TestLoadRuleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.sql")
	content := "" +
		"# a comment line — ignored\n" +
		"\n" + // blank — ignored
		"成年: age >= 18\n" + // labelled (CJK name)
		"city IN ('深圳','广州')\n" + // no label -> name "rule 2"
		"   phone IS NOT NULL   \n" + // surrounding space trimmed
		"# another comment\n" +
		"JSON城市: JSON_EXTRACT(profile, '$.city') = '深圳'\n" // label + colon-free expr
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	rules, err := engine.LoadRulesAuto(path) // .sql -> line mode
	if err != nil {
		t.Fatalf("LoadRulesAuto: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("got %d rules, want 4 (comments/blank skipped)", len(rules))
	}

	// IDs sequential from 1, in file order.
	for i, r := range rules {
		if r.ID != int64(i+1) {
			t.Errorf("rule[%d].ID = %d, want %d", i, r.ID, i+1)
		}
		if !r.Enabled {
			t.Errorf("rule[%d] should be enabled", i)
		}
	}

	// Labelled line: name + expr split; expr must not keep the label.
	if rules[0].Name != "成年" || rules[0].Expr != "age >= 18" {
		t.Errorf("labelled rule = %q / %q, want 成年 / age >= 18", rules[0].Name, rules[0].Expr)
	}
	// Unlabelled line: default name, full text as expr.
	if rules[1].Name != "rule 2" || rules[1].Expr != "city IN ('深圳','广州')" {
		t.Errorf("unlabelled rule = %q / %q", rules[1].Name, rules[1].Expr)
	}
	// Trimmed.
	if rules[2].Expr != "phone IS NOT NULL" {
		t.Errorf("trim failed: %q", rules[2].Expr)
	}
	// Label + an expr that itself contains no ": " — the JSON path keeps intact.
	if rules[3].Name != "JSON城市" || rules[3].Expr != "JSON_EXTRACT(profile, '$.city') = '深圳'" {
		t.Errorf("json rule = %q / %q", rules[3].Name, rules[3].Expr)
	}

	// And they compile under the native front-end.
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	if loaded, failed := eng.LoadRules(rules); failed != 0 || loaded != 4 {
		t.Errorf("compile: loaded=%d failed=%d, want 4/0", loaded, failed)
	}
}
