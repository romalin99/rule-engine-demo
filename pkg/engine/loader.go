package engine

import (
	"encoding/json"
	"fmt"
	"os"

	"tcg-rulex-engine/pkg/model"
)

// LoadRules reads and unmarshals a rules JSON file.
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
