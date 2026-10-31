const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { repoRoot, targets, validateVersion } = require('./package-layout');
const archive = require('./archive');

function run(bin, args, options = {}) {
  const result = spawnSync(bin, args, { cwd: repoRoot, stdio: 'inherit', windowsHide: true, ...options });
  if (result.error || result.status !== 0) throw result.error || new Error(`${bin} failed: ${result.status}`);
}
function safeOutput(file) {
  if (fs.existsSync(file) && fs.lstatSync(file).isSymbolicLink()) throw new Error('Linked installer output forbidden');
  return file;
}
function copyTree(source, destination) {
  if (fs.lstatSync(source).isSymbolicLink()) throw new Error('Linked installer resource forbidden');
  fs.mkdirSync(destination, { recursive: true });
  for (const entry of fs.readdirSync(source, { withFileTypes: true })) {
    const from = path.join(source, entry.name), to = path.join(destination, entry.name);
    if (entry.isSymbolicLink()) throw new Error('Linked installer resource forbidden');
    if (entry.isDirectory()) copyTree(from, to);
    else if (entry.isFile()) fs.copyFileSync(from, to);
    else throw new Error('Nonregular installer resource forbidden');
  }
}
function cleanStage(output, name) {
  const staging = path.resolve(output, '.installers', name);
  if (!path.relative(output, staging).startsWith(`.installers${path.sep}`)) throw new Error('Unsafe installer staging path');
  for (const parent of [output, path.dirname(staging), staging]) {
    if (fs.existsSync(parent) && fs.lstatSync(parent).isSymbolicLink()) throw new Error('Linked installer staging forbidden');
  }
  fs.rmSync(staging, { recursive: true, force: true });
  fs.mkdirSync(staging, { recursive: true });
  return staging;
}
function listFiles(root, prefix = '') {
  return fs.readdirSync(root, { withFileTypes: true }).sort((a,b)=>a.name.localeCompare(b.name)).flatMap(entry => {
    const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isSymbolicLink()) throw new Error('Linked installer file forbidden');
    if (entry.isDirectory()) return listFiles(path.join(root, entry.name), relative);
    if (!entry.isFile() || /[\r\n\\]/.test(relative)) throw new Error('Invalid installer resource name');
    return [relative];
  });
}
function ar(output, files) {
  const chunks = [Buffer.from('!<arch>\n')];
  for (const [name, data] of files) {
    const header = `${(name+'/').padEnd(16)}${'0'.padEnd(12)}${'0'.padEnd(6)}${'0'.padEnd(6)}${'100644'.padEnd(8)}${String(data.length).padEnd(10)}\x60\n`;
    if (header.length !== 60) throw new Error('Invalid ar header');
    chunks.push(Buffer.from(header), data);
    if (data.length % 2) chunks.push(Buffer.from('\n'));
  }
  fs.writeFileSync(output, Buffer.concat(chunks));
}
function buildLinux(payload, output, version, arch) {
  const name = `learn-with-ai-${version}-linux-${arch}`;
  const staging = cleanStage(output, name);
  const bundle = path.join(staging, 'bundle');
  copyTree(payload, path.join(bundle, 'app'));
  fs.copyFileSync(path.join(__dirname, 'linux-install.sh'), path.join(bundle, 'install.sh'));
  fs.copyFileSync(path.join(__dirname, 'linux-uninstall.sh'), path.join(bundle, 'uninstall.sh'));
  fs.writeFileSync(path.join(bundle, '.learn-with-ai-files'), listFiles(path.join(bundle, 'app')).map(file=>`app/${file}`).join('\n')+'\n');
  const installArchive = safeOutput(path.join(output, `${name}-install.tar.gz`));
  archive.tarGzip(bundle, name, installArchive, ['install.sh','uninstall.sh','app/learn-with-ai']);

  const data = path.join(staging, 'data'), control = path.join(staging, 'control');
  copyTree(payload, path.join(data, 'opt', 'learn-with-ai'));
  fs.mkdirSync(path.join(data, 'usr', 'bin'), { recursive: true });
  fs.mkdirSync(path.join(data, 'usr', 'share', 'applications'), { recursive: true });
  fs.writeFileSync(path.join(data, 'usr', 'bin', 'learn-with-ai'), '#!/bin/sh\nexec /opt/learn-with-ai/learn-with-ai "$@"\n');
  fs.writeFileSync(path.join(data, 'usr', 'share', 'applications', 'learn-with-ai.desktop'), '[Desktop Entry]\nType=Application\nName=Learn-With-AI\nExec=/opt/learn-with-ai/learn-with-ai\nTerminal=false\nCategories=Education;\n');
  fs.mkdirSync(control);
  fs.writeFileSync(path.join(control, 'control'), `Package: learn-with-ai\nVersion: ${version}\nArchitecture: ${arch}\nMaintainer: learn-with-ai contributors <noreply@learnwithai.local>\nSection: education\nPriority: optional\nRecommends: tesseract-ocr, tesseract-ocr-chi-sim\nDescription: Local AI learning workbench\n Go server with browser interface and built-in document parsing.\n`);
  archive.tarGzip(data, '.', path.join(staging, 'data.tar.gz'), ['opt/learn-with-ai/learn-with-ai','usr/bin/learn-with-ai']);
  archive.tarGzip(control, '.', path.join(staging, 'control.tar.gz'), []);
  const deb = safeOutput(path.join(output, `${name}.deb`));
  ar(deb, [['debian-binary', Buffer.from('2.0\n')], ['control.tar.gz', fs.readFileSync(path.join(staging, 'control.tar.gz'))], ['data.tar.gz', fs.readFileSync(path.join(staging, 'data.tar.gz'))]]);
  return [deb, installArchive];
}
function buildMacApp(payload, output, version, arch) {
  const staging = cleanStage(output, `learn-with-ai-${version}-darwin-${arch}`);
  const app = path.join(staging, 'root', 'learn-with-ai.app');
  copyTree(payload, path.join(app, 'Contents', 'MacOS'));
  fs.chmodSync(path.join(app, 'Contents', 'MacOS', 'learn-with-ai'), 0o755);
  fs.writeFileSync(path.join(app, 'Contents', 'Info.plist'), `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleName</key><string>Learn-With-AI</string><key>CFBundleDisplayName</key><string>Learn-With-AI</string><key>CFBundleIdentifier</key><string>com.learnwithai.app</string><key>CFBundleExecutable</key><string>learn-with-ai</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>${version.split('-')[0]}</string><key>CFBundleVersion</key><string>${version.split('-')[0]}</string><key>LSMinimumSystemVersion</key><string>12.0</string><key>LSArchitecturePriority</key><array><string>${arch==='amd64'?'x86_64':'arm64'}</string></array></dict></plist>`);
  return app;
}
function buildWindows(payload, output, version, arch) { return require('./windows-msi').buildMsi(payload, output, version, arch); }
function main() {
  const value = (flag, fallback) => { const index=process.argv.indexOf(flag); return index<0?fallback:process.argv[index+1]; };
  const version = validateVersion(value('--version',require('../../package.json').version));
  const host = {win32:'windows',darwin:'darwin',linux:'linux'}[process.platform];
  const selected = value('--targets',targets.filter(target=>target.startsWith(`${host}/`)).join(',')).split(',');
  if (!selected.length || selected.some(target=>!targets.includes(target))) throw new Error('Invalid installer targets');
  const output = path.join(repoRoot,'dist','releases',version);
  const files = [];
  for (const target of selected) {
    const [goos,arch]=target.split('/');
    const payload = path.join(output,'.staging',`learn-with-ai-${version}-${goos}-${arch}`);
    if (!fs.existsSync(payload)) throw new Error(`Build portable payload first: ${target}`);
    if (goos==='windows') files.push(...buildWindows(payload,output,version,arch));
    else if (goos==='linux') files.push(...buildLinux(payload,output,version,arch));
    else {
      const app=buildMacApp(payload,output,version,arch);
      const tar=safeOutput(path.join(output,`learn-with-ai-${version}-darwin-${arch}-app.tar.gz`));
      archive.tarGzip(path.dirname(app),`learn-with-ai-${version}-darwin-${arch}`,tar,['learn-with-ai.app/Contents/MacOS/learn-with-ai','learn-with-ai.app/Contents/MacOS/libexec/lll-vision-ocr','learn-with-ai.app/Contents/MacOS/libexec/lll-speech']);
      files.push(tar);
      if (process.platform==='darwin') {
        const pkg=safeOutput(path.join(output,`learn-with-ai-${version}-darwin-${arch}.pkg`));
        run('pkgbuild',['--root',path.dirname(app),'--identifier','com.learnwithai.app','--version',version.split('-')[0],'--install-location','/Applications',pkg]);
        files.push(pkg);
      } else console.log('macOS .app prepared; .pkg compilation requires the native macOS CI job');
    }
  }
  for (const file of files) console.log(`Installer: ${file}`);
}
module.exports = { buildLinux, buildMacApp };
if (require.main===module) main();
