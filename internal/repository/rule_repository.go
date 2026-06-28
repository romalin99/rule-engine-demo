// file today, an Oracle table later — behind the RuleRepository interface, so the
// service/engine never depend on the storage mechanism.
package repository

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

// DefaultRulesPath is the default JSON rule source for the file repository.
const DefaultRulesPath = "data/rules.json"

// RuleRepository abstracts the source of rule definitions. Implementations return
// rules in the engine's model.Rule shape.
type RuleRepository interface {
	Load(ctx context.Context) ([]model.Rule, error)
}

// FileRuleRepository loads rules from a JSON file (e.g. data/rules.json,
// data/rules_30.json). It reuses the engine's file loader.
type FileRuleRepository struct{ Path string }

// NewFileRuleRepository returns a file-backed rule repository.
func NewFileRuleRepository(path string) *FileRuleRepository {
	if path == "" {
		path = DefaultRulesPath
	}
	return &FileRuleRepository{Path: path}
}

// Load reads and parses the JSON rule file into []model.Rule.
func (r *FileRuleRepository) Load(_ context.Context) ([]model.Rule, error) {
	return engine.LoadRules(r.Path)
}

// DBRuleRepository is an Oracle-backed rule source (DB layer, mirrors ucs-fe
// repositories). Skeleton: wire the real query when a rules table exists.
type DBRuleRepository struct{ db *sqlx.DB }

// NewDBRuleRepository returns a DB-backed rule repository.
func NewDBRuleRepository(db *sqlx.DB) *DBRuleRepository { return &DBRuleRepository{db: db} }

// Load is a placeholder for the DB-backed rule query.
//
// TODO: build with goqu (oracle dialect, as in ucs-fe repos) and scan into
// []model.Rule, e.g.:
//
//	SELECT id, name, expr, priority, enabled FROM tcg_rulex.rules WHERE enabled = 1
func (r *DBRuleRepository) Load(_ context.Context) ([]model.Rule, error) {
	return nil, fmt.Errorf("DBRuleRepository.Load: not implemented (skeleton) — wire the rules table query")
}
