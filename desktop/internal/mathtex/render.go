// Package mathtex typesets untrusted TeX offline. The embedded MathJax bundle
// runs in an isolated JavaScript VM, never a browser or a system TeX process.
package mathtex

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

//go:embed renderer.js
var javascript string

const MaxSource = 8192
const maxSVG = 256 * 1024

var compiled = sync.OnceValues(func() (*goja.Program, error) {
	return goja.Compile("herdr-math.js", javascript, true)
})

type Formula struct {
	SVG                  []byte
	Width, Height, Depth float32 // ems; Height is above the baseline.
}

type Renderer struct {
	vm     *goja.Runtime
	render goja.Callable
	fonts  *fontOutlines
}

func (r *Renderer) Render(ctx context.Context, source string, display bool) (*Formula, error) {
	if len(source) > MaxSource || strings.TrimSpace(source) == "" {
		return nil, errors.New("formula is empty or too long")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.vm == nil {
		program, err := compiled()
		if err != nil {
			return nil, err
		}
		vm := goja.New()
		if _, err = vm.RunProgram(program); err != nil {
			return nil, err
		}
		render, ok := goja.AssertFunction(vm.Get("renderMath"))
		if !ok {
			return nil, errors.New("missing math renderer")
		}
		r.vm, r.render = vm, render
	}
	// A broken or adversarial formula cannot monopolize the single worker.
	deadline, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	stopped := make(chan struct{})
	stop := context.AfterFunc(deadline, func() { r.vm.Interrupt("formula timed out"); close(stopped) })
	value, err := r.render(goja.Undefined(), r.vm.ToValue(source), r.vm.ToValue(display))
	if !stop() {
		<-stopped
	}
	r.vm.ClearInterrupt()
	if err != nil {
		return nil, fmt.Errorf("typeset formula: %w", err)
	}
	if err := deadline.Err(); err != nil {
		return nil, err
	}
	svg := value.String()
	if len(svg) > maxSVG {
		return nil, errors.New("formula output too large")
	}
	if r.fonts == nil {
		r.fonts = &fontOutlines{}
	}
	return r.fonts.convert(svg)
}
