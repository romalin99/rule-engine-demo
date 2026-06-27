// Package native is the dependency-free SQL-subset parser front-end. It uses
// the hand-written lexer/parser in pkg/ir and emits the unified IR directly.
package native

import (
	"github.com/example/rule-engine-demo/pkg/api"
	"github.com/example/rule-engine-demo/pkg/ir"
)

// Parser implements api.Parser using the project's own SQL parser.
type Parser struct{}

// New returns a native SQL parser.
func New() *Parser { return &Parser{} }

// Name identifies the parser.
func (*Parser) Name() string { return "native-sql" }

// Parse parses SQL-WHERE rule text into the unified IR.
func (*Parser) Parse(rule string) (api.Program, error) { return ir.Parse(rule) }
