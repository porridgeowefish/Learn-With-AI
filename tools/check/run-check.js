// Iteration-14 quality gate runner. One place that defines what
// check:fast / check / check:full mean, so local and CI results agree.
//
//   fast     formatting + vet + build + archcheck + focused Go tests + frontend lint + frontend tests
//   complete fast + full Go tests + production builds + coverage floors
//   full     complete + contract suite + Windows-native smoke (local Windows only)
const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const mode = process.argv[2] || 'fast';
if (!['fast', 'complete', 'full'].includes(mode)) {
  console.error('usage: node tools/check/run-check.js fast|complete|full');
  process.exit(2);
}

const artifacts = path.join('trash', 'verification', `check-${mode}-${process.pid}`);
const temporary = path.resolve(artifacts, 'tmp');
fs.mkdirSync(temporary, { recursive: true });
const verificationEnv = { ...process.env, TEMP: temporary, TMP: temporary, TMPDIR: temporary };
// Go does not use .gitignore for ./...; a nested module keeps temporary Go
// fixtures and compiler scratch files outside the repository package set.
const trashModule = path.join('trash', 'go.mod');
if (!fs.existsSync(trashModule)) fs.writeFileSync(trashModule, 'module learn-with-ai-trash\n\ngo 1.26\n');
console.log(`Temporary verification artifacts: ${artifacts}`);
fs.mkdirSync(path.join(artifacts, 'tests'), { recursive: true });
fs.mkdirSync(path.join(artifacts, 'coverage'), { recursive: true });
fs.mkdirSync(path.join(artifacts, 'architecture'), { recursive: true });

let failed = false;

function run(title, command, args, opts = {}) {
  if (failed && !opts.always) {
    console.log(`-- skipping ${title} (earlier failure)`);
    return false;
  }
  console.log(`\n=== ${title} ===`);
  const result = spawnSync(command, args, {
    stdio: 'inherit',
    // Only npm's Windows shim needs cmd.exe. Native executables (including
    // Node under Program Files) must preserve argv and their full path.
    shell: process.platform === 'win32' && command === 'npm',
    env: { ...verificationEnv, FORCE_COLOR: '0' },
    ...opts.spawn,
  });
  const ok = result.status === 0;
  if (!ok) {
    console.error(`FAIL: ${title}`);
    failed = true;
  }
  return ok;
}

// ---- shared fast layer ----
run('repository hygiene', 'node', ['tools/check/check-repository-hygiene.js']);

(function gofmtAssert() {
  console.log('\n=== gofmt check ===');
  const result = spawnSync('gofmt', ['-l', 'backend-go', 'tools'], { encoding: 'utf8' });
  if (result.status !== 0) {
    console.error('FAIL: gofmt could not run');
    failed = true;
  } else if (result.stdout.trim() !== '') {
    console.error('FAIL: these files are not gofmt-formatted:\n' + result.stdout);
    failed = true;
  } else {
    console.log('clean');
  }
})();

run('go vet', 'go', ['vet', './backend-go/...', './tools/...']);
run('go build', 'go', ['build', './...']);
run('archcheck', 'go', ['run', './tools/archcheck', '-repo', '.', '-report', `${artifacts}/architecture/dependency-report.json`]);
run('Go tests (short)', 'go', ['test', '-short', '-count=1', '-timeout', '600s', './backend-go/...', './tools/...']);
run('frontend lint', 'npm', ['--prefix', 'frontend', 'run', 'lint']);
run('frontend tests', 'npm', ['--prefix', 'frontend', 'run', 'test']);

// ---- complete adds ----
if (mode !== 'fast') {
  run(
    'Go tests (full, coverage)',
    'go',
    ['test', '-count=1', '-timeout', '900s', '-coverprofile', `${artifacts}/coverage/go.out`, './backend-go/...'],
  );
  if (!failed) run('Go coverage gate', 'node', ['tools/check/check-coverage.js', 'go', `${artifacts}/coverage/go.out`]);
  run('frontend production build', 'npm', ['--prefix', 'frontend', 'run', 'build']);
  run('backend production build', process.execPath, ['tools/release/manage.js', 'build']);
}

// ---- full adds ----
if (mode === 'full') {
  // Run the frozen HTTP/SSE/persistence contract subset explicitly.
  run(
    'contract freeze suite',
    'go',
    ['test', '-count=1', '-timeout', '300s', './backend-go/internal/transport/httpserver/'],
  );
  if (process.platform === 'win32') {
    run(
      'Windows desktop launcher health regression',
      'node',
      ['--test', 'tools/release/desktop-launcher.test.js'],
    );
} else {
    console.log('\n=== Windows-native smoke === skipped (not on Windows)');
  }
}

console.log(failed ? `\ncheck:${mode} FAILED` : `\ncheck:${mode} PASSED`);
process.exit(failed ? 1 : 0);
