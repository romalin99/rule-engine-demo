// Package runtime is the root of the pluggable rule-runtime tree. The Runtime
// contract and the Plan/Program types are defined in pkg/api and re-exported
// here per the project layout; concrete implementations live in the
// sub-packages (bytecode, ast, qlbridge, cel, expr).
//
// A small name→constructor registry mirrors pkg/parser so a runtime can be
// selected by string from config/CLI.
package runtime

import (
	"sort"

	"github.com/example/rule-engine-demo/pkg/api"
)

// Runtime compiles and executes Programs. Alias of api.Runtime.
type Runtime = api.Runtime

// Program is the unified IR consumed by a Runtime. Alias of api.Program.
type Program = api.Program

// Plan is a runtime-specific compiled artifact. Alias of api.Plan.
type Plan = api.Plan

var registry = map[string]func() Runtime{}

// Register adds a named runtime constructor (typically called from init()).
func Register(name string, ctor func() Runtime) { registry[name] = ctor }

// New constructs a registered runtime by name.
func New(name string) (Runtime, bool) {
	ctor, ok := registry[name]
	if !ok {
		return nil, false
	}
	return ctor(), true
}

// Names lists the registered runtime names, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
