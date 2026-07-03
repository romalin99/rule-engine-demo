// safecall_test.go — round fifteen: builtin panic containment. The registry
// contract says every builtin is total, but SafeCall is the enforcement: a
// panicking implementation (a latent bug, or a third-party library surprise
// inside USERAGENT/HASH_SIP) must degrade to NULL — never reach a worker
// goroutine, where a panic without recover kills the process.
package sqlfn

import "testing"

func TestSafeCallContainsPanic(t *testing.T) {
	bomb := &Builtin{Name: "TEST_BOMB", Min: 0, Max: -1, Fn: func([]any) any {
		panic("builtin bug")
	}}
	before := RecoveredPanics()
	if out := SafeCall(bomb, nil); out != nil {
		t.Fatalf("SafeCall(panicking builtin) = %v, want nil", out)
	}
	if got := RecoveredPanics() - before; got != 1 {
		t.Fatalf("RecoveredPanics delta = %d, want 1", got)
	}
	// nil-typed panic values are contained too (recover() != nil is not the
	// guard — the guard fires on any panic, including panic(nil) semantics
	// under Go 1.21+ where recover returns *runtime.PanicNilError).
	nilBomb := &Builtin{Name: "TEST_NIL_BOMB", Fn: func([]any) any { panic(nil) }, Max: -1}
	if out := SafeCall(nilBomb, nil); out != nil {
		t.Fatalf("SafeCall(panic(nil) builtin) = %v, want nil", out)
	}
	// a healthy builtin passes through untouched
	ok := &Builtin{Name: "TEST_OK", Fn: func(a []any) any { return "x" }, Max: -1}
	if out := SafeCall(ok, nil); out != "x" {
		t.Fatalf("SafeCall(healthy builtin) = %v, want x", out)
	}
	if got := RecoveredPanics() - before; got != 2 {
		t.Fatalf("RecoveredPanics total delta = %d, want 2", got)
	}
}
