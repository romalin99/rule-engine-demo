// Package vm is a tiny stack-based bytecode virtual machine for boolean rule
// expressions. It is completely independent of any parser: it consumes an IR
// tree (package ir), compiles it to flat bytecode, and evaluates that bytecode
// against a map[string]any row.
//
//	IR ──Compile──▶ Program(ByteCode) ──Eval(row)──▶ bool
//
// Swapping the front-end parser (qlbridge / SQLParser / CEL / Expr) changes only
// how the IR is produced; this VM stays the same.
package vm

// OpCode is a single bytecode instruction kind.
type OpCode uint8

const (
	OpLoadField    OpCode = iota // A=field idx:   push row[field]
	OpConstNum                   // A=num idx:     push number const
	OpConstStr                   // A=str idx:     push string const
	OpEq                         // pop b,a:       push a == b
	OpNe                         // pop b,a:       push a != b
	OpGt                         // pop b,a:       push a > b
	OpGe                         // pop b,a:       push a >= b
	OpLt                         // pop b,a:       push a < b
	OpLe                         // pop b,a:       push a <= b
	OpBetween                    // pop hi,lo,x:   push lo <= x <= hi
	OpIn                         // A=set idx:     pop x, push x in set
	OpLikePrefix                 // A=str idx:     pop x, push hasPrefix(x, s)
	OpLikeSuffix                 // A=str idx:     pop x, push hasSuffix(x, s)
	OpLikeContains               // A=str idx:     pop x, push contains(x, s)
	OpLikeEq                     // A=str idx:     pop x, push x == s
	OpIsNull                     // pop x:         push x is null/missing
	OpIsNotNull                  // pop x:         push x is present/non-null
	OpAnd                        // pop b,a:       push a && b
	OpOr                         // pop b,a:       push a || b
	OpNot                        // pop a:         push !a  (NOT / NOT IN / NOT LIKE)
)

// Instr is one instruction: an opcode plus a single operand (an index into one
// of the Program's constant pools, unused for stack-only ops).
type Instr struct {
	Op OpCode
	A  int32
}

// Program is compiled bytecode plus its constant pools.
type Program struct {
	Source string
	Code   []Instr
	Nums   []float64
	Strs   []string
	Fields []string
	Sets   []map[string]struct{}
}

// Len reports the number of instructions.
func (p *Program) Len() int { return len(p.Code) }
