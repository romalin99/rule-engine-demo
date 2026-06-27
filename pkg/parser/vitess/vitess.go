// Package vitess is a placeholder for a Vitess SQL-parser front-end.
//
// Status: stub. It implements api.Parser so it can be registered and swapped in,
// but Parse returns a not-implemented error. It is intentionally dependency-free
// so the module still builds without pulling in the (very large) Vitess tree.
//
// To complete it:
//  1. add `vitess.io/vitess/go/vt/sqlparser` (run `go mod tidy`);
//  2. parse the WHERE clause:
//     stmt, _ := sqlparser.Parse("select 1 where " + rule)
//     where := stmt.(*sqlparser.Select).Where.Expr
//  3. walk the sqlparser.Expr tree (AndExpr/OrExpr/ComparisonExpr/RangeCond/
//     ComparisonExpr-IN/LikeExpr) into ir.Node, exactly like pkg/parser/qlbridge
//     does for the qlbridge AST.
package vitess

import (
	"fmt"

	"github.com/example/rule-engine-demo/pkg/api"
)

// Parser implements api.Parser via Vitess (not yet implemented).
type Parser struct{}

// New returns a Vitess front-end stub.
func New() *Parser { return &Parser{} }

// Name identifies the parser.
func (*Parser) Name() string { return "vitess" }

// Parse is not implemented yet; see the package doc for the intended approach.
func (*Parser) Parse(rule string) (api.Program, error) {
	return nil, fmt.Errorf("vitess parser not implemented yet: add vitess.io/vitess/go/vt/sqlparser and walk its WHERE-clause AST into ir.Node")
}
