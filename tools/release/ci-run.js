// Runs one CI command and, when it fails, republishes the end of its output
// (plus matching installer logs) as GitHub annotations. Annotations stay
// readable through the REST API even when raw job logs are not reachable.
//
//   node tools/release/ci-run.js [--temp-logs <prefix>] -- <command> [args...]
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

const argv = process.argv.slice(2);
const split = argv.indexOf('--');
if (split < 0 || split === argv.length - 1) {
  console.error('usage: ci-run.js [--temp-logs <prefix>] -- <command> [args...]');
  process.exit(2);
}
const options = argv.slice(0, split);
const [command, ...args] = argv.slice(split + 1);
const prefixIndex = options.indexOf('--temp-logs');
const tempPrefix = prefixIndex >= 0 ? options[prefixIndex + 1] : '';
require('./temporary-root').prepareTemporaryRoot();

function escape(value) {
  return value.replace(/%/g, '%25').replace(/\r/g, '').replace(/\n/g, '%0A').replace(/::/g, ': :');
}
function annotate(title, lines) {
  const body = lines.filter(line => line.trim() !== '').slice(-60).join('\n');
  if (body) console.log(`::error title=${escape(title).replace(/,/g, ' ')}::${escape(body)}`);
}
function readLog(file) {
  const raw = fs.readFileSync(file);
  return raw[0] === 0xff && raw[1] === 0xfe ? raw.toString('utf16le') : raw.toString('utf8');
}

let output = '';
const child = spawn(command, args, { stdio: ['ignore', 'pipe', 'pipe'], shell: process.platform === 'win32' && /^(npm|npx)$/.test(command) });
for (const stream of [child.stdout, child.stderr]) {
  stream.on('data', chunk => {
    output += chunk;
    if (output.length > 1 << 20) output = output.slice(-(1 << 19));
    (stream === child.stdout ? process.stdout : process.stderr).write(chunk);
  });
}
child.on('error', error => {
  annotate(`${command} could not start`, [error.message]);
  process.exit(1);
});
child.on('close', code => {
  if (code === 0) process.exit(0);
  annotate(`${path.basename(command)} exited ${code}`, output.split('\n'));
  if (tempPrefix) {
    const folders = fs.readdirSync(os.tmpdir(), { withFileTypes: true })
      .filter(entry => entry.isDirectory() && entry.name.startsWith(tempPrefix))
      .map(entry => path.join(os.tmpdir(), entry.name))
      .sort((a, b) => fs.statSync(b).mtimeMs - fs.statSync(a).mtimeMs);
    if (folders[0]) {
      for (const name of fs.readdirSync(folders[0]).filter(file => file.endsWith('.log'))) {
        const lines = readLog(path.join(folders[0], name)).split(/\r?\n/);
        annotate(name, lines.filter(line => /return value 3|error|failed|exception/i.test(line)).slice(-25));
      }
    }
  }
  process.exit(code ?? 1);
});
