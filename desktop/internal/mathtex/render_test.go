package mathtex

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRenderer(t *testing.T) {
	var r Renderer
	for _, source := range []string{`p(A\mid\mathbf x)`, `\prod_{n=0}^{N-1}\frac{1}{\sqrt{2\pi\sigma^2}}\exp\!\left[-\frac{(x[n]-A)^2}{2\sigma^2}\right]`, `\underbrace{-\frac{1}{2\sigma^2}\sum_{n=0}^{N-1}(x[n]-A)^2}_{\text{来自观测：}A\text{ 与数据有多接近}}`, `\begin{pmatrix}a&b\\c&d\end{pmatrix}`, `A\sim\mathcal N(\mu_A,\sigma_A^2)`} {
		t.Run(source, func(t *testing.T) {
			start := time.Now()
			f, err := r.Render(context.Background(), source, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Log("duration", time.Since(start), "dimensions", f.Width, f.Height, f.Depth)
			if f.Width <= 0 || f.Height+f.Depth <= 0 || !strings.Contains(string(f.SVG), "<path") || strings.Contains(string(f.SVG), "<text") {
				t.Fatalf("invalid formula: %+v", f)
			}
		})
	}
}

func TestUnsupportedAndOversizedFormulasFallBack(t *testing.T) {
	var r Renderer
	for _, source := range []string{`\unknown{x}`, `\frac{1}{`, `\href{https://example.com}{x}`, `\require{html}`, `\def\x{\x}\x`, strings.Repeat("x", MaxSource+1)} {
		if _, err := r.Render(context.Background(), source, false); err == nil {
			t.Errorf("accepted %q", source)
		}
	}
	if _, err := r.Render(context.Background(), `x^2`, false); err != nil {
		t.Fatal("error poisoned next render", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Render(ctx, `x`, true); err == nil {
		t.Fatal("ignored cancellation")
	}
}
