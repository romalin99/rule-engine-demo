// Package parser is the root of the pluggable rule-parser tree. The Parser
// contract and the Program type are defined in pkg/api and re-exported here so
// callers can depend on `parser.Parser` per the project layout; concrete
// implementations live in the sub-packages (qlbridge, native, json, expr, cel,
// vitess).
//
// A tiny name→constructor registry lets callers select a parser by string
// (e.g. from a config file or CLI flag) without importing every sub-package.
// Sub-packages may self-register in an init(); dependency-heavy ones (expr/cel)
// are left for the caller to register explicitly so their deps stay opt-in.
package parser

import (
	"sort"

	"github.com/example/rule-engine-demo/pkg/api"
)

// Parser turns rule text into a Program (unified IR). Alias of api.Parser.
type Parser = api.Parser

// Program is the unified IR produced by a Parser. Alias of api.Program.
type Program = api.Program

var registry = map[string]func() Parser{}

// Register adds a named parser constructor (typically called from init()).
func Register(name string, ctor func() Parser) { registry[name] = ctor }

// New constructs a registered parser by name.
func New(name string) (Parser, bool) {
	ctor, ok := registry[name]
	if !ok {
		return nil, false
	}
	return ctor(), true
}

// Names lists the registered parser names, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
