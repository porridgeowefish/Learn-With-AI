import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import base from '../vite.config.ts';

const source = fileURLToPath(new URL('.', import.meta.url));
const modules = () => ['worker.mjs', 'runtime.mjs', 'page-adapter.mjs', 'projects.mjs', 'learning.mjs', 'settings.mjs', 'snapshot.mjs', 'visuals.mjs', 'cloud.mjs'];
const files = () => [...modules(), 'fflate.mjs', 'treasures.json', 'starter-dialogue.json'];
const filePath = name => name === 'treasures.json' ? resolve(source, '../../backend-go/internal/modules/treasures/catalog.json')
  : name === 'starter-dialogue.json' ? resolve(source, '../../backend-go/internal/app/workspaceservice/starter-dialogue.json')
  : name === 'fflate.mjs' ? resolve(source, '../node_modules/fflate/esm/browser.js') : resolve(source, name);
const urlFor = name => name === 'worker.mjs' ? '/design-worker.mjs' : '/__design/' + name;
const revision = () => {
  const hash = createHash('sha256');
  for (const name of [...modules(), 'starter-dialogue.json']) hash.update(readFileSync(filePath(name)));
  return hash.digest('hex').slice(0, 16);
};

export default {
  ...base,
  plugins: [...base.plugins, {
    name: 'isolated-design-preview',
    transformIndexHtml: { order: 'pre', handler(html) {
      return html.replace('/src/app/main.tsx', '/design/entry.mjs')
        .replace('<title>Learn-With-AI</title>', `<title>Learn-With-AI · 设计预览</title><meta name="lll-design-revision" content="${revision()}">`);
    }},
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const path = req.url?.split('?')[0];
        const name = files().find(file => urlFor(file) === path);
        if (name) {
          res.setHeader('Content-Type', name.endsWith('.json') ? 'application/json' : 'text/javascript; charset=utf-8');
          res.setHeader('Cache-Control', 'no-store');
          res.end(readFileSync(filePath(name)));
        } else if (/^\/(api|files|events)(\/|$)/.test(path)) {
          res.statusCode = 503;
          res.end('Design worker unavailable');
        } else next();
      });
      server.watcher.add(filePath('starter-dialogue.json'));
      server.watcher.on('change', file => {
        if ([...modules(), 'starter-dialogue.json'].some(name => filePath(name) === resolve(file))) server.ws.send({ type: 'custom', event: 'design-data-changed' });
      });
    },
    generateBundle() {
      for (const name of files()) this.emitFile({ type: 'asset', fileName: urlFor(name).slice(1), source: readFileSync(filePath(name)) });
    },
  }],
  server: { host: '127.0.0.1', port: 5188, strictPort: true },
  preview: { host: '127.0.0.1', port: 5188, strictPort: true },
  build: { ...base.build, outDir: 'design/dist' },
};
