package mathtex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func awaitFormula(t *testing.T, c *Cache, source string) *Formula {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if f, ready := c.Get(source, false); ready {
			return f
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("formula worker timed out")
	return nil
}
func TestCacheBoundsFailuresReuseAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewCache(ctx)
	f := awaitFormula(t, c, `x^2`)
	if f == nil {
		t.Fatal("missing formula")
	}
	if again, ready := c.Get(`x^2`, false); !ready || again != f {
		t.Fatal("did not reuse cached formula")
	}
	if awaitFormula(t, c, `\unknown{x}`) != nil {
		t.Fatal("invalid formula rendered")
	}
	revision, _ := c.State()
	if failed, ready := c.Get(`\unknown{x}`, false); !ready || failed != nil {
		t.Fatal("failed result not cached")
	}
	if next, _ := c.State(); next != revision {
		t.Fatal("retried failed render")
	}
	for i := 0; i < cacheLimit+4; i++ {
		if awaitFormula(t, c, fmt.Sprintf(`x_{%d}`, i)) == nil {
			t.Fatal(i)
		}
	}
	c.mu.Lock()
	if len(c.order) > cacheLimit || len(c.entries) > cacheLimit || c.bytes > cacheBytes {
		t.Error("unbounded cache")
	}
	if _, ok := c.entries[key{`x^2`, false}]; ok {
		t.Error("old result retained")
	}
	c.mu.Unlock()
	cancel()
	if f, ready := c.Get(`unseen`, false); f != nil || !ready {
		t.Fatal("cancelled cache accepted work")
	}
}
func TestRuntimeInterruptDoesNotPoisonLaterRequests(t *testing.T) {
	vm := goja.New()
	value, err := vm.RunString(`(function(){while(true){}})`)
	if err != nil {
		t.Fatal(err)
	}
	fn, _ := goja.AssertFunction(value)
	r := Renderer{vm: vm, render: fn}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := r.Render(ctx, `x`, false); err == nil {
		t.Fatal("did not interrupt renderer")
	}
	value, err = vm.RunString(`(function(){return '<svg viewBox="0 -800 500 1000"><path d="M0 0L500 0L0 800Z"/></svg>';})`)
	if err != nil {
		t.Fatal(err)
	}
	r.render, _ = goja.AssertFunction(value)
	if _, err = r.Render(context.Background(), `x`, false); err != nil {
		t.Fatal("VM remained interrupted", err)
	}
}
