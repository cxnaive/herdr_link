import {readFileSync} from 'node:fs';
import {build} from 'esbuild';

const notice = `MathJax 3.2.2, Copyright (c) 2017-2022 The MathJax Consortium.\n\n${readFileSync('../MATHJAX-LICENSE', 'utf8')}\n\nGoja JavaScript interpreter:\n${readFileSync('../GOJA-LICENSE', 'utf8')}`;
await build({
  entryPoints: ['render.js'], bundle: true, minify: true,
  platform: 'neutral', mainFields: ['main'], target: 'es2015',
  banner: {js: `/*!\n${notice}\n*/`}, outfile: '../renderer.js',
});
