import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

// Review the design adapter whenever a product route or API path changes.
const root = fileURLToPath(new URL('../src/', import.meta.url));
const contract = new Set();
function scan(dir) {
  for (const entry of fs.readdirSync(dir, {withFileTypes:true})) {
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== 'test') scan(file);
      continue;
    }
    if (!/\.tsx?$/.test(file) || /\.(test|spec|d)\.tsx?$/.test(file)) continue;
    const src = ts.createSourceFile(file, fs.readFileSync(file,'utf8'), ts.ScriptTarget.Latest, true);
    function visit(n) {
      if (ts.isStringLiteralLike(n) || ts.isTemplateExpression(n)) {
        const value = ts.isTemplateExpression(n) ? n.head.text + n.templateSpans.map(s=>'{}'+s.literal.text).join('') : n.text;
        if (/^(?:\{\})?\/(?:api|files)(?:\/|$)/.test(value)) contract.add('api ' + value.replace(/^\{\}/,''));
      }
      if (file.endsWith('router.tsx') && ts.isPropertyAssignment(n) && n.name.getText(src) === 'path') contract.add('page '+n.initializer.getText(src));
      ts.forEachChild(n,visit);
    }
    visit(src);
  }
}
scan(root);
const dest = new URL('./product-contract.txt', import.meta.url);
const actual = [...contract].sort().join('\n')+'\n';
if (process.argv.includes('--record')) fs.writeFileSync(dest,actual);
else if (fs.readFileSync(dest,'utf8').replaceAll('\r\n','\n') !== actual) {
  throw Error('产品路由或接口已改变。更新对应设计数据和操作，验收后运行 node frontend/design/check.mjs --record。');
}
console.log('Design preview product routes and API paths: '+contract.size);
