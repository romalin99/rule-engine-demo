// Package dtable implements a Decision Table input: a tabular rule format where
// each row is a conjunction of cell conditions. A table compiles to ordinary
// model.Rule values (SQL text), so it reuses the whole engine pipeline
// (Frontend → IR → ByteCode → VM) with no special casing.
//
//	Decision Table (rows) ─▶ IR ─▶ SQL ─▶ []model.Rule ─▶ Engine
package dtable

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/example/rule-engine-demo/pkg/ir"
	"github.com/example/rule-engine-demo/pkg/model"
)

// Cond is a single cell: a predicate on one field.
//
//	op ∈ { =, ==, !=, >, >=, <, <=, between, in, like, isnull, isnotnull }
type Cond struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
	Values []any  `json:"values,omitempty"`
}

// Row is one decision-table row: all conditions AND-ed together.
type Row struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Enabled  *bool  `json:"enabled"`
	When     []Cond `json:"when"`
}

// Table is a decision table.
type Table struct {
	Name   string `json:"name"`
	IDBase int64  `json:"id_base"`
	Rows   []Row  `json:"rows"`
}

// Load reads a decision table from a JSON file.
func Load(path string) (Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Table{}, fmt.Errorf("dtable read %s: %w", path, err)
	}
	var t Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return Table{}, fmt.Errorf("dtable parse %s: %w", path, err)
	}
	return t, nil
}

// Rules compiles the table into engine rules. Each row becomes one rule whose
// expression is the SQL rendering of its AND-ed conditions.
func (t Table) Rules() ([]model.Rule, error) {
	rules := make([]model.Rule, 0, len(t.Rows))
	for i, row := range t.Rows {
		node, err := row.toIR()
		if err != nil {
			return nil, fmt.Errorf("row %d (%s): %w", i, row.Name, err)
		}
		enabled := row.Enabled == nil || *row.Enabled
		rules = append(rules, model.Rule{
			ID:       t.IDBase + int64(i),
			Name:     row.Name,
			Expr:     ir.Emit(node, ir.SQL),
			Priority: row.Priority,
			Enabled:  enabled,
		})
	}
	return rules, nil
}

func (r Row) toIR() (ir.Node, error) {
	if len(r.When) == 0 {
		return nil, fmt.Errorf("empty row")
	}
	args := make([]ir.Node, 0, len(r.When))
	for _, c := range r.When {
		n, err := c.toIR()
		if err != nil {
			return nil, err
		}
		args = append(args, n)
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return ir.Logic{Op: "AND", Args: args}, nil
}

func (c Cond) toIR() (ir.Node, error) {
	switch c.Op {
	case "=", "==", "!=", ">", ">=", "<", "<=":
		v, err := toVal(c.Value)
		if err != nil {
			return nil, err
		}
		op := c.Op
		if op == "==" {
			op = "="
		}
		return ir.Compare{Field: c.Field, Op: op, Val: v}, nil
	case "between":
		if len(c.Values) != 2 {
			return nil, fmt.Errorf("between needs 2 values")
		}
		lo, err := toVal(c.Values[0])
		if err != nil {
			return nil, err
		}
		hi, err := toVal(c.Values[1])
		if err != nil {
			return nil, err
		}
		return ir.Between{Field: c.Field, Lo: lo, Hi: hi}, nil
	case "in":
		vals := make([]ir.Value, 0, len(c.Values))
		for _, raw := range c.Values {
			v, err := toVal(raw)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		return ir.In{Field: c.Field, Vals: vals}, nil
	case "like":
		s, ok := c.Value.(string)
		if !ok {
			return nil, fmt.Errorf("like value must be string")
		}
		return ir.Like{Field: c.Field, Pattern: s}, nil
	case "isnull":
		return ir.IsNull{Field: c.Field, Negate: false}, nil
	case "isnotnull":
		return ir.IsNull{Field: c.Field, Negate: true}, nil
	}
	return nil, fmt.Errorf("unsupported op %q", c.Op)
}

func toVal(v any) (ir.Value, error) {
	switch x := v.(type) {
	case string:
		return ir.Value{IsString: true, Str: x}, nil
	case float64:
		return ir.Value{Num: strconv.FormatFloat(x, 'f', -1, 64)}, nil
	case bool:
		return ir.Value{IsString: true, Str: strconv.FormatBool(x)}, nil
	case json.Number:
		return ir.Value{Num: x.String()}, nil
	}
	return ir.Value{}, fmt.Errorf("unsupported value %T", v)
}
