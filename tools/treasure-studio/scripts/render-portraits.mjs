import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';
import { createServer } from 'vite';

const studio = new URL('../', import.meta.url);
const output = new URL('../../../frontend/public/treasures/assets/portraits/', import.meta.url);
const manifest = JSON.parse(await readFile(new URL('public/assets/manifest.json', studio), 'utf8'));
const ids = process.argv.slice(2);
const expected = new Set(manifest.assets.filter(item => !ids.length || ids.includes(item.id)).map(item => item.id));
if (ids.some(id => !expected.has(id))) throw new Error('An asset ID was not found in the treasure catalog.');
await mkdir(output, { recursive: true });

const server = await createServer({ root: fileURLToPath(studio), server: { host: '127.0.0.1', port: 0, strictPort: true } });
try {
  await server.listen();
  const address = server.httpServer.address();
  const browser = await chromium.launch({ channel: 'chrome', args: ['--enable-unsafe-swiftshader'] });
  try {
    const page = await browser.newPage();
    await page.exposeFunction('savePortrait', async (id, url) => {
      if (!expected.delete(id) || !url.startsWith('data:image/webp;base64,')) throw new Error(`Invalid portrait: ${id}`);
      await writeFile(new URL(`${id}.webp`, output), Buffer.from(url.split(',')[1], 'base64'));
      console.log(`Rendered ${id}`);
    });
    const surface = `http://127.0.0.1:${address.port}/__portrait_authoring`;
    await page.route(surface, route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Treasure portrait rendering</title>' }));
    await page.goto(surface);
    await page.evaluate(async requested => {
      const { renderThumbnails } = await import('/src/thumbnails.js');
      await renderThumbnails(window.savePortrait, { size: 1024, quality: .98, ids: requested.length ? requested : undefined });
    }, ids);
    if (expected.size) throw new Error(`Missing portraits: ${[...expected].join(', ')}`);
  } finally { await browser.close(); }
} finally { await server.close(); }
