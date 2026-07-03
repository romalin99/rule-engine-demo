package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tcg-rulex-engine/pkg/model"
)

// LoadRulesAuto reads a rule-set, dispatching by file extension:
//
//   - .json  → a JSON rule-set array ([]model.Rule); the rule/expr field holds
//     the DSL text (SQL, or a JSON-rule document string for the JSON front-end).
//   - .sql / .cel / .expr / .txt (anything else) → a plain-text file with ONE
//     rule expression per line, `#` line-comments and blank lines ignored.
//
// The plain-text form makes per-DSL example files like SQL(native).sql,
// rules.cel or rules.expr directly loadable: pick the matching front-end
// (-frontend native|cel|expr|…) and every non-comment line becomes one rule.
func LoadRulesAuto(path string) ([]model.Rule, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return LoadRules(path)
	}
	return LoadRuleLines(path)
}

// LoadRules reads and unmarshals a JSON rule-set file ([]model.Rule).
func LoadRules(path string) ([]model.Rule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules file %s: %w", path, err)
	}
	var rules []model.Rule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("parse rules file %s: %w", path, err)
	}
	return rules, nil
}

// LoadRuleLines reads a plain-text rule file: one rule expression per line, in
// the DSL of whatever front-end will parse it. Blank lines and lines whose
// first non-space rune is '#' are ignored (comments). An optional leading
// "<name>: " prefix names that rule; otherwise the name is "rule N". IDs are
// assigned sequentially from 1 in file order. No rule text is otherwise
// altered — the front-end parses it verbatim.
func LoadRuleLines(path string) ([]model.Rule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules file %s: %w", path, err)
	}
	var rules []model.Rule
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow long rule lines
	id := int64(0)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id++
		name := fmt.Sprintf("rule %d", id)
		expr := line
		// Optional "name: expr" label — only when the text BEFORE the first
		// ": " is a simple label (so a JSON path or operator is never mistaken
		// for one).
		if i := strings.Index(line, ": "); i > 0 && isPlainLabel(line[:i]) {
			name = strings.TrimSpace(line[:i])
			expr = strings.TrimSpace(line[i+2:])
		}
		rules = append(rules, model.Rule{ID: id, Name: name, Expr: expr, Enabled: true})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read rules file %s: %w", path, err)
	}
	return rules, nil
}

// isPlainLabel reports whether s is a simple identifier-like label (letters,
// digits, spaces, underscores, hyphens, CJK) — used to tell an optional
// "name: expr" prefix apart from rule text that merely contains a colon.
func isPlainLabel(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 60 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == ' ', r >= 0x4E00 && r <= 0x9FFF: // CJK
		default:
			return false
		}
	}
	return true
}

// LoadUsers reads and unmarshals a users JSON file (array of flat objects).
func LoadUsers(path string) ([]model.User, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read users file %s: %w", path, err)
	}
	var users []model.User
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil, fmt.Errorf("parse users file %s: %w", path, err)
	}
	// Backfill UIDs if the file omitted them (e.g. the sample users.json).
	for i := range users {
		if users[i].UID == 0 {
			users[i].UID = int64(i + 1)
		}
	}
	return users, nil
}

// SaveRules writes rules as pretty JSON (used to persist generated rule sets).
func SaveRules(path string, rules []model.Rule) error {
	return writeJSON(path, rules)
}

// SaveUsers writes users as pretty JSON.
func SaveUsers(path string, users []model.User) error {
	return writeJSON(path, users)
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
