const fs = require('node:fs');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '../..');
const binary = path.join(root, 'dist', 'dev', process.platform === 'win32' ? 'learn-with-ai.exe' : 'learn-with-ai');
const action = process.argv[2];
if (action === 'build-desktop') {
  if (process.platform !== 'win32') throw new Error('Native desktop developer build currently requires Windows');
  const desktop = path.join(root, 'dist', 'dev', 'learn-with-ai-desktop.exe');
  fs.mkdirSync(path.dirname(desktop), { recursive: true });
  const version = require('../../package.json').version + '-dev';
  const built = spawnSync('go', ['build', '-ldflags', `-H windowsgui -X main.Version=${version}`, '-o', desktop, './backend-go/cmd/desktop'], { cwd: root, stdio: 'inherit', windowsHide: true });
  if (built.status !== 0) process.exitCode = built.status ?? 1;
  else {
    const resources = spawnSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', path.join(__dirname, 'windows-manifest.ps1'), '-Executable', desktop, '-Version', version], { cwd: root, stdio: 'inherit', windowsHide: true });
    process.exitCode = resources.status ?? 1;
  }
} else if (action === 'build') {
  fs.mkdirSync(path.dirname(binary), { recursive: true });
  const version = require('../../package.json').version + '-dev';
  const result = spawnSync('go', ['build', '-ldflags', `-X main.Version=${version}`, '-o', binary, './backend-go/cmd/lll'], { cwd: root, stdio: 'inherit', windowsHide: true });
  process.exitCode = result.status ?? 1;
} else if (action === 'start') {
  const child = spawn(binary, process.argv.slice(3), { cwd: root, stdio: 'inherit' });
  child.on('error', error => { console.error(error.message); process.exitCode = 1; });
  child.on('exit', code => { process.exitCode = code ?? 1; });
} else if (action === 'stop') {
  const port = process.env.LLL_SERVER_PORT || '8787';
  fetch(`http://127.0.0.1:${port}/api/system/shutdown`, { method: 'POST' }).then(response => {
    if (!response.ok) throw new Error(`Shutdown returned ${response.status}`);
    console.log('learn-with-ai stopped');
  }).catch(error => { console.error(error.message); process.exitCode = 1; });
} else {
  throw new Error('Expected build, build-desktop, start or stop');
}
