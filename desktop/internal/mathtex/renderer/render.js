// Runs inside a Goja VM with no filesystem, network, DOM, or Node bindings.
import {mathjax} from 'mathjax-full/js/mathjax.js';
import {TeX} from 'mathjax-full/js/input/tex.js';
import {SVG} from 'mathjax-full/js/output/svg.js';
import {liteAdaptor} from 'mathjax-full/js/adaptors/liteAdaptor.js';
import {RegisterHTMLHandler} from 'mathjax-full/js/handlers/html.js';
import 'mathjax-full/js/input/tex/base/BaseConfiguration.js';
import 'mathjax-full/js/input/tex/ams/AmsConfiguration.js';
import 'mathjax-full/js/input/tex/boldsymbol/BoldsymbolConfiguration.js';
import 'mathjax-full/js/input/tex/textmacros/TextMacrosConfiguration.js';

const adaptor = liteAdaptor();
RegisterHTMLHandler(adaptor);
const tex = new TeX({
  packages: ['base', 'ams', 'boldsymbol', 'textmacros'],
  maxBuffer: 8192,
  maxMacros: 256,
  formatError(_jax, error) { throw error; },
});
const doc = mathjax.document('', {InputJax: tex, OutputJax: new SVG({fontCache: 'none'})});
globalThis.renderMath = (source, display) => {
  tex.reset();
  const node = doc.convert(source, {display, em: 16, ex: 8, containerWidth: 1280});
  return adaptor.outerHTML(adaptor.firstChild(node));
};
