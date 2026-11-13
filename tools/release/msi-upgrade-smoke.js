const assert = require('node:assert/strict');
const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');
const { createHash } = require('node:crypto');
const { spawn, spawnSync } = require('node:child_process');
const { once } = require('node:events');
const { setTimeout: wait } = require('node:timers/promises');
const { msiIdentity } = require('./windows-msi');

const previousVersion = '0.10.12';
const nextVersion = require('../../package.json').version;
const oldDigest = '63b7301af9d2326c07250dbd68f98b860f4f13c691ab8469576692455d4148ee';
const slug = 'installer-upgrade-fixture';
const title = 'Existing learning project';
const quote = value => `'${String(value).replaceAll("'", "''")}'`;
const sha256 = data => createHash('sha256').update(data).digest('hex');

function powershell(script) {
  const command = Buffer.from(`$ErrorActionPreference='Stop'; ${script}`, 'utf16le').toString('base64');
  const result = spawnSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-EncodedCommand', command], { encoding: 'utf8', windowsHide: true, timeout: 30000 });
  assert.equal(result.status, 0, result.stderr || result.error?.message);
  return result.stdout.trim();
}

function metadata(msi) {
  return JSON.parse(powershell(`$i=New-Object -ComObject WindowsInstaller.Installer; $db=$i.OpenDatabase(${quote(msi)},0); $out=@{};
    $view=$db.OpenView('SELECT Property, Value FROM Property'); $view.Execute();
    while($row=$view.Fetch()){ if($row.StringData(1) -in 'ProductCode','ProductVersion','UpgradeCode'){ $out[$row.StringData(1)]=$row.StringData(2) } }; $out | ConvertTo-Json -Compress`));
}

function registered(code) {
  return powershell(`$i=New-Object -ComObject WindowsInstaller.Installer; $i.ProductState(${quote(code)})`) === '5';
}

function runMsi(action, target, log, accept) {
  const result = spawnSync('msiexec.exe', [action, target, '/qn', '/norestart', '/l*v', log], { windowsHide: true, timeout: 180000 });
  assert(accept.includes(result.status), `msiexec ${action} exited ${result.status ?? result.error?.message}; inspect ${log}`);
}

async function startDesktop(binary, scratch, workspace, version) {
  const listener = net.createServer();
  listener.listen(0, '127.0.0.1');
  await once(listener, 'listening');
  const { port } = listener.address();
  await new Promise(resolve => listener.close(resolve));
  const child = spawn(binary, ['--no-browser', '--port', String(port), '--workspace', workspace], {
    cwd: scratch, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, WORKSPACE: '', LWA_WORKSPACE_ROOT: '', LLL_WORKSPACE_ROOT: '', LWA_CLOUD_URL: 'http://127.0.0.1:1' },
  });
  let output = '';
  const exited = once(child, 'exit');
  child.stdout.on('data', data => output += data);
  child.stderr.on('data', data => output += data);
  const base = `http://127.0.0.1:${port}`;
  const stop = async () => {
    if (child.exitCode === null) {
      try { await fetch(base + '/api/system/shutdown', { method: 'POST', signal: AbortSignal.timeout(2000) }); } catch { /* terminate this fixture if HTTP is unavailable */ }
      await Promise.race([exited, wait(3000)]);
      if (child.exitCode === null && child.signalCode === null) child.kill();
    }
    await exited;
    fs.writeFileSync(path.join(scratch, `desktop-${version}.log`), output);
  };
  try {
    for (let attempt = 0; attempt < 180 && child.exitCode === null; attempt++) {
      let health;
      try { health = await (await fetch(base + '/api/health', { signal: AbortSignal.timeout(1000) })).json(); } catch { /* server is starting */ }
      if (health?.ok) {
        assert.equal(path.resolve(health.workspace), workspace);
        assert(output.includes(`Learn-With-AI ${version}`), output);
        return { base, stop };
      }
      await wait(100);
    }
    throw Error(`Installed ${version} did not start: ${output}`);
  } catch (error) {
    await stop();
    throw error;
  }
}

async function main([oldFile, newFile, mode]) {
  assert.equal(process.platform, 'win32');
  assert(oldFile && (newFile || mode === '--inspect'), 'usage: msi-upgrade-smoke.js <old.msi> [new.msi] [--inspect]');
  const oldPath = path.resolve(oldFile);
  const nextPath = newFile && newFile !== '--inspect' ? path.resolve(newFile) : undefined;
  const inspect = mode === '--inspect' || newFile === '--inspect';
  assert.equal(sha256(fs.readFileSync(oldPath)), oldDigest, 'Previous official MSI digest mismatch');
  const old = metadata(oldPath);
  const family = `{${msiIdentity(previousVersion, 'amd64').upgradeCode}}`;
  assert.equal(old.ProductVersion, previousVersion);
  assert.equal(old.UpgradeCode.toUpperCase(), family);
  const next = nextPath && metadata(nextPath);
  if (next) {
    assert.equal(next.ProductVersion, nextVersion);
    assert.equal(next.UpgradeCode.toUpperCase(), family);
    assert.notEqual(next.ProductCode.toUpperCase(), old.ProductCode.toUpperCase());
  }
  for (const { ProductCode } of [old, next].filter(Boolean)) assert.match(ProductCode, /^\{[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\}$/i);
  console.log(`Validated ${old.ProductVersion}${next ? ' → ' + next.ProductVersion : ''} release MSI metadata and previous official digest`);
  if (inspect) return;
  assert(next, 'Candidate MSI is required');
  assert.equal(process.env.GITHUB_ACTIONS, 'true', 'Installation requires GitHub Actions');
  assert.equal(process.env.RUNNER_ENVIRONMENT, 'github-hosted', 'Installation requires an ephemeral GitHub-hosted runner');
  require('./temporary-root').prepareTemporaryRoot();
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'learn-msi-upgrade-'));
  const workspace = path.join(scratch, 'workspace');
  const binary = path.join(process.env.LOCALAPPDATA, 'Programs', 'learn-with-ai', 'learn-with-ai.exe');
  const installerKey = "HKCU:\\Software\\learn-with-ai\\Installer";
  const products = powershell(`$i=New-Object -ComObject WindowsInstaller.Installer; @($i.RelatedProducts(${quote(family)})).Count`);
  assert.equal(products, '0', 'Runner already has a Learn-With-AI product');
  assert(!fs.existsSync(binary), 'Runner already has a Learn-With-AI binary');
  assert.equal(powershell(`Test-Path -LiteralPath ${quote(installerKey)}`), 'False', 'Runner already has Learn-With-AI installer registry data');

  let desktop;
  try {
    runMsi('/i', oldPath, path.join(scratch, 'old-install.log'), [0, 3010]);
    assert(registered(old.ProductCode));
    desktop = await startDesktop(binary, scratch, workspace, old.ProductVersion);
    const response = await fetch(desktop.base + '/api/projects', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slug, title, projectType: 'system-learning' }),
    });
    const created = await response.text();
    assert(response.ok, created);
    await desktop.stop();
    desktop = undefined;
    const project = path.join(workspace, 'projects', slug);
    const sentinel = path.join(project, 'retained-learner-notes.txt');
    const note = 'Learner-owned data persisted by the previous installation.\n';
    fs.writeFileSync(sentinel, note);
    const state = path.join(project, 'state.json');
    const previousHash = sha256(fs.readFileSync(state));
    fs.writeFileSync(path.join(path.dirname(binary), 'retained-install-note.txt'), note);

    const upgradeLog = path.join(scratch, 'upgrade.log');
    runMsi('/i', nextPath, upgradeLog, [0, 3010]);
    assert(!registered(old.ProductCode), 'Windows Installer left the old ProductCode installed');
    assert(registered(next.ProductCode), 'Windows Installer did not register the new ProductCode');
    assert.equal(powershell(`(Get-ItemProperty -LiteralPath ${quote(installerKey)}).Version`), next.ProductVersion);
    assert.equal(sha256(fs.readFileSync(state)), previousHash);
    assert.equal(fs.readFileSync(sentinel, 'utf8'), note);
    assert.equal(fs.readFileSync(path.join(path.dirname(binary), 'retained-install-note.txt'), 'utf8'), note);

    desktop = await startDesktop(binary, scratch, workspace, next.ProductVersion);
    const list = await (await fetch(desktop.base + '/api/projects')).json();
    assert(list.projects.some(project => project.slug === slug && project.title === title), 'Existing learner project was lost after upgrade');
    await desktop.stop();
    desktop = undefined;
    runMsi('/x', next.ProductCode, path.join(scratch, 'uninstall.log'), [0, 3010]);
    assert(!registered(next.ProductCode) && !fs.existsSync(binary));
    assert.equal(fs.readFileSync(sentinel, 'utf8'), note);
    assert.equal(sha256(fs.readFileSync(state)), previousHash);
    console.log(`Actual Windows MSI upgrade ${previousVersion} → ${nextVersion} replaced the prior product and kept learner data after upgrade and uninstall`);
  } finally {
    if (desktop) await desktop.stop();
    for (const { ProductCode } of [next, old]) {
      if (registered(ProductCode)) runMsi('/x', ProductCode, path.join(scratch, `cleanup-${ProductCode}.log`), [0, 3010, 1605]);
    }
  }
}

main(process.argv.slice(2)).catch(error => { console.error(error); process.exitCode = 1; });
