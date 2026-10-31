const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { repoRoot, targets, packagePaths, validateVersion } = require('./package-layout');
const archive = require('./archive');

function value(flag, fallback) {
  const index = process.argv.indexOf(flag);
  return index < 0 ? fallback : process.argv[index + 1];
}
function run(bin, args, options = {}) {
  const result = spawnSync(bin, args, { cwd: repoRoot, stdio: 'inherit', windowsHide: true, ...options });
  if (result.error || result.status !== 0) throw result.error || new Error(`${bin} failed: ${result.status}`);
}
function main() {
  const version = validateVersion(value('--version', require('../../package.json').version));
  const selected = value('--targets', targets.join(',')).split(',');
  if (!selected.length || selected.some(target => !targets.includes(target))) throw new Error('Invalid release targets');
  if (!process.argv.includes('--skip-frontend')) run('npm', ['--prefix', 'frontend', 'run', 'build'], { shell: process.platform === 'win32' });
  const releaseRoot = path.resolve(repoRoot, 'dist', 'releases', version);
  for (const directory of [path.join(repoRoot,'dist'),path.join(repoRoot,'dist','releases'),releaseRoot,path.join(releaseRoot,'.staging')]) {
      if (fs.existsSync(directory) && fs.lstatSync(directory).isSymbolicLink()) throw new Error('Linked output parent forbidden');
    }
  fs.mkdirSync(releaseRoot, { recursive: true });
  if (fs.lstatSync(releaseRoot).isSymbolicLink()) throw new Error('Linked release directory forbidden');
  for (const target of selected) {
    const [goos, arch] = target.split('/');
    const prefix = `learn-with-ai-${version}-${goos}-${arch}`;
    const staging = path.resolve(releaseRoot, '.staging', prefix);
    const relative = path.relative(releaseRoot, staging);
    if (relative.startsWith('..') || path.isAbsolute(relative)) throw new Error('Unsafe staging path');
    if (fs.existsSync(staging)) {
      if (fs.lstatSync(staging).isSymbolicLink()) throw new Error('Linked staging forbidden');
      fs.rmSync(staging, { recursive: true, force: true });
    }
    fs.mkdirSync(staging, { recursive: true });
    const names = packagePaths(goos, arch);
    const binary = path.join(staging, names[0]);
    console.log(`Building ${target}`);
    run('go', ['build', '-trimpath', '-ldflags', `${goos === 'windows' ? '-H windowsgui ' : ''}-s -w -X github.com/xmz14/lll/backend-go/internal/platform/filesystem.BuildMode=release -X main.Version=${version}`, '-o', binary, goos === 'windows' ? './backend-go/cmd/desktop' : './backend-go/cmd/lll'], { env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: arch } });
    if (goos === 'windows') {
      if (process.platform !== 'win32') throw new Error('Windows GUI resource embedding requires Windows');
      run('powershell.exe', ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', path.join(__dirname, 'windows-manifest.ps1'), '-Executable', binary, '-IconPath', path.join(repoRoot, 'scripts', 'assets', 'learn-with-ai.ico'), '-Version', version]);
    }
    if (goos === 'darwin') {
      if (process.platform !== 'darwin') throw new Error('macOS Vision component requires the native macOS build job');
      const helper = path.join(staging, 'libexec', 'lll-vision-ocr');
      fs.mkdirSync(path.dirname(helper), { recursive: true });
      run('xcrun', ['clang', '-fobjc-arc', '-O2', '-arch', arch === 'amd64' ? 'x86_64' : 'arm64', '-mmacosx-version-min=12.0', '-framework', 'Foundation', '-framework', 'Vision', path.join(__dirname, 'native', 'mac-vision.m'), '-o', helper]);
      fs.chmodSync(helper, 0o755);
      const speech = path.join(staging, 'libexec', 'lll-speech');
      run('xcrun', ['clang', '-fobjc-arc', '-fblocks', '-O2', '-arch', arch === 'amd64' ? 'x86_64' : 'arm64', '-mmacosx-version-min=12.0', '-framework', 'Foundation', '-framework', 'Speech', '-Wl,-sectcreate,__TEXT,__info_plist,' + path.join(__dirname, 'native', 'mac-speech.plist'), path.join(__dirname, 'native', 'mac-speech.m'), '-o', speech]);
      fs.chmodSync(speech, 0o755);
    }
    fs.chmodSync(binary, 0o755);
    for (const name of names.slice(1)) {
      const destination = path.join(staging, name.startsWith('docs/release/') ? path.basename(name) : name);
      fs.mkdirSync(path.dirname(destination), { recursive: true });
      fs.copyFileSync(path.join(repoRoot, name), destination);
    }
    if (goos === ({ win32: 'windows', darwin: 'darwin', linux: 'linux' })[process.platform] && arch === ({ x64: 'amd64', arm64: 'arm64' })[process.arch]) {
      run(process.execPath, [path.join(__dirname, 'smoke.js'), binary]);
    }
    const filename = `${prefix}${goos === 'windows' ? '.zip' : '.tar.gz'}`;
    const output = path.join(releaseRoot, filename);
    if (fs.existsSync(output) && fs.lstatSync(output).isSymbolicLink()) throw new Error('Linked archive output forbidden');
    if (goos === 'windows') archive.zip(staging, prefix, output);
    else archive.tarGzip(staging, prefix, output, goos === 'darwin' ? ['learn-with-ai', 'libexec/lll-vision-ocr', 'libexec/lll-speech'] : ['learn-with-ai']);
    console.log(`Packaged ${filename}`);
  }
  if (process.argv.includes('--installers')) {
    const installerTargets = selected.filter(target => !target.startsWith('windows/') || process.platform === 'win32');
    if (installerTargets.length) run(process.execPath, [path.join(__dirname, 'build-installers.js'), '--version', version, '--targets', installerTargets.join(',')]);
    if (installerTargets.length !== selected.length) console.log('Windows installers require the native Windows build job; portable Windows payloads are ready.');
  }
}
if (require.main === module) main();
