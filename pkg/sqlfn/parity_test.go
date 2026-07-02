// parity_test.go — hardening regression for the URL_MATCHQS pattern cache
// (round seven): pattern arguments may come from row data (a rule can pass a
// field), so the memoization must be bounded or hostile rows could grow
// process memory without limit.
package sqlfn

import (
	"fmt"
	"testing"
)

func TestRegexpCacheBounded(t *testing.T) {
	before := reCacheN.Load()
	// Hammer the cache with far more distinct (valid) patterns than the cap.
	for i := 0; i < reCacheMax+200; i++ {
		re, err := regexpCached(fmt.Sprintf("^key_%d$", i))
		if err != nil {
			t.Fatalf("pattern %d: %v", i, err)
		}
		if re == nil || !re.MatchString(fmt.Sprintf("key_%d", i)) {
			t.Fatalf("pattern %d compiled but does not match", i)
		}
	}
	if n := reCacheN.Load(); n > reCacheMax+8 { // tiny racer overshoot allowed
		t.Errorf("cache grew past its bound: %d > %d", n, reCacheMax)
	}
	if before > 0 && reCacheN.Load() < before {
		t.Error("cache counter went backwards")
	}
	// Beyond the cap, compilation still works (uncached) and bad patterns
	// still report errors.
	if _, err := regexpCached("("); err == nil {
		t.Error("expected compile error for '('")
	}
}

func TestURLMatchQSDynamicPattern(t *testing.T) {
	// The pattern may be any term — here simulated as an already-evaluated
	// argument, the way both runtimes hand it to the builtin.
	b, ok := Lookup("URL_MATCHQS")
	if !ok {
		t.Fatal("URL_MATCHQS not registered")
	}
	got := b.Fn([]any{"http://x.com/p?utm_a=1&drop=2", "^utm_"})
	if got != "x.com/p?utm_a=1" {
		t.Errorf("got %v", got)
	}
	if b.Fn([]any{"http://x.com/p?a=1", "("}) != nil { // bad dynamic pattern -> NULL
		t.Error("bad pattern should yield NULL")
	}
}
