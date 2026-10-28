const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const net = require('node:net');
const { spawn } = require('node:child_process');

async function smoke(binary) {
  require('./temporary-root').prepareTemporaryRoot();
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'learn-with-ai-smoke-'));
  const workspace = path.join(scratch, 'workspace');
  const socket = net.createServer();
  await new Promise(resolve => socket.listen(0, '127.0.0.1', resolve));
  const port = socket.address().port;
  await new Promise(resolve => socket.close(resolve));
  const invalid = path.join(scratch, 'missing-cli');
  const child = spawn(path.resolve(binary), ['--no-browser', '--port', String(port), '--workspace', workspace], {
    cwd: scratch, windowsHide: true,
    env: { ...process.env, WORKSPACE:workspace, LLL_WORKSPACE_ROOT:workspace, CLAUDE_BIN: invalid, CODEX_BIN: invalid, CODEBUDDY_BIN: invalid, WORKBUDDY_BIN: invalid, HERMES_BIN: invalid, TRAE_BIN: invalid },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let log = '', exited = false;
  child.stdout.on('data', data => { log += data; });
  child.stderr.on('data', data => { log += data; });
  child.on('exit', () => { exited = true; });
  const base = `http://127.0.0.1:${port}`;
  try {
    const deadline = Date.now() + 30000;
    let health;
    while (Date.now() < deadline && !exited) {
      try { const response = await fetch(`${base}/api/health`); if (response.ok) { health = await response.json(); break; } } catch { /* startup */ }
      await new Promise(resolve => setTimeout(resolve, 150));
    }
    assert(health?.ok, `Portable server did not start: ${log}`);
    assert.equal(path.resolve(health.workspace), path.resolve(workspace));
    const index = await fetch(base);
    assert.equal(index.status, 200);
    assert.match(await index.text(), /Learn-With-AI/);
    // The installed application must ship the real collectible assets without seeded rewards.
    const collection = await (await fetch(`${base}/api/treasures`)).json();
    assert.equal(collection.catalog.length, 50);
    assert.deepEqual(collection.rewards, []);
    assert(!fs.existsSync(path.join(workspace, 'treasures', 'ledger.json')), 'Startup seeded a reward ledger');
    for (const item of collection.catalog) {
      for (const resource of [item.url, `assets/thumbs/${item.id}.webp`]) {
        const asset = await fetch(`${base}/treasures/${resource}`, { method: 'HEAD' });
        assert.equal(asset.status, 200, `Packaged collectible missing: ${resource}`);
        assert(!asset.headers.get('content-type')?.includes('text/html'), `SPA fallback masked missing asset: ${resource}`);
        assert(Number(asset.headers.get('content-length')) > 12, `Empty collectible: ${resource}`);
      }
    }
    for (const name of ['01-lagoon','02-sunset','03-moonlight','04-volcanic','05-mediterranean','06-pink-sand']) {
      const background = await fetch(`${base}/treasures/references/${name}.png`, { method:'HEAD' });
      assert.equal(background.status, 200);
      assert.match(background.headers.get('content-type'), /image\/png/);
    }
    const ocean = await fetch(`${base}/treasures/assets/ocean/spectral-atlas.png`, { method:'HEAD' });
    assert.equal(ocean.status, 200);
    assert.match(ocean.headers.get('content-type'), /image\/png/);
    const retired = await fetch(`${base}/api/projects/nonexistent/generated`);
    assert.equal(retired.status, 404);
    // A different port must not bypass the workspace's single-writer boundary.
    const duplicate = spawn(path.resolve(binary), ['--no-browser', '--port', String(port === 65535 ? port-1 : port+1), '--workspace', workspace], { cwd: scratch, windowsHide: true, env: {...process.env, WORKSPACE:workspace, LLL_WORKSPACE_ROOT:workspace, CLAUDE_BIN:invalid,CODEX_BIN:invalid,CODEBUDDY_BIN:invalid,WORKBUDDY_BIN:invalid,HERMES_BIN:invalid,TRAE_BIN:invalid}, stdio:['ignore','pipe','pipe'] });
    let duplicateLog='';
    duplicate.stdout.on('data',data=>{duplicateLog+=data;});duplicate.stderr.on('data',data=>{duplicateLog+=data;});
    const duplicateCode=await new Promise((resolve,reject)=>{const timer=setTimeout(()=>{duplicate.kill();reject(new Error('Duplicate instance failed to exit'));},10000);duplicate.once('error',reject);duplicate.once('exit',code=>{clearTimeout(timer);resolve(code);});});
    assert.notEqual(duplicateCode,0);assert.match(duplicateLog,/workspace is already open/);
    const stopped = await fetch(`${base}/api/system/shutdown`, { method: 'POST' });
    assert.equal(stopped.status, 202);
    for (let i = 0; i < 40 && !exited; i++) await new Promise(resolve => setTimeout(resolve, 100));
    assert(exited, 'UI shutdown did not terminate portable server');
    console.log('Portable startup, SPA, retired API, duplicate-instance exclusion and shutdown smoke passed');
  } finally {
    if (!exited) { child.kill(); await new Promise(resolve => {child.once('exit',resolve);setTimeout(resolve,5000);}); }
    // Only the scratch directory created by this invocation is removed.
    const relative = path.relative(os.tmpdir(), scratch);
    if (!relative.startsWith('..') && relative.startsWith('learn-with-ai-smoke-')) fs.rmSync(scratch, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
}
smoke(process.argv[2]).catch(error => { console.error(error.message); process.exitCode = 1; });
