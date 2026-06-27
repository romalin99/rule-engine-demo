package engine

// Extension points for additional parser front-ends live in their own files:
//
//	frontend.go           NativeFrontend  (project SQL-subset parser)
//	frontend_qlbridge.go  QLBridgeFrontend (qlbridge SQL parser → IR)
//	frontend_json.go      JSONFrontend     (JSON rule objects → IR)
//	frontend_cel.go       CELFrontend      (Google CEL → IR)
//	frontend_expr.go      ExprFrontend     (expr-lang/expr → IR)
//
// To add another language, implement Frontend.Parse(rule) (ir.Node, error) and
// register it in newEngine (benchmark.go). The IR → ByteCode → VM pipeline and
// all business code stay unchanged.
