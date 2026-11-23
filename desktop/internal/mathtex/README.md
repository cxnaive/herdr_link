# Offline native mathematics

The desktop Markdown hooks use MathJax **3.2.2**'s TeX → SVG renderer inside the
pure-Go **Goja** interpreter (exact revision in `desktop/go.mod`). Goja adds no
Node, browser, filesystem, network or subprocess bindings. It is needed because
MyGo exposes math rendering hooks but does not include a TeX layout engine.
No Node installation, WebView, CDN, system TeX or external service is required
in a packaged Windows, Linux or macOS client.

Only base, AMS, boldsymbol and textmacros are compiled in; dynamic packages,
HTML and URL extensions are not enabled. One background worker processes bounded
requests. Source is limited to 8 KiB, output to 256 KiB, each conversion to 350 ms,
and the cache to 128 results / 16 MiB. Errors remain readable as source. Results
are kept only in memory, never on disk. Fonts/paths use the current UI color.

MathJax supplies vector paths for mathematical symbols. Its Unicode `<text>`
fallbacks (including Chinese `\text{…}`) are converted to outlines with the
already-used go-text system-font scanner, because MyGo SVG intentionally does
not render `<text>`. As for the rest of the UI, a system CJK font is required
for Chinese. A missing glyph falls back to source, rather than disappearing.

`renderer.js` is generated and committed so normal Go/CI builds require no npm
step. Rebuild only when editing the renderer or upgrading its dependencies:

```sh
cd desktop/internal/mathtex/renderer
npm ci --ignore-scripts
npm run build
```

The lockfile pins build inputs. MathJax's Apache-2.0 license and Goja's MIT
license are included in this directory and in the generated bundle's banner,
which is embedded in the executable. MathJax source:
https://github.com/mathjax/MathJax-src/tree/3.2.2 .
The shared regression fixture is `testdata/client-math/bayes.md`.
