const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '../..');
const targets = ['windows/amd64', 'windows/arm64', 'darwin/amd64', 'darwin/arm64', 'linux/amd64', 'linux/arm64'];

function validateVersion(version) {
  if (!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(version)) throw new Error('Invalid release version');
  return version;
}

function packagePaths(goos, arch, root = repoRoot) {
  if (!targets.includes(`${goos}/${arch}`)) throw new Error('Unsupported release target');
  const names = [goos === 'windows' ? 'learn-with-ai.exe' : 'learn-with-ai'];
  function walk(relative) {
    const absolute = path.join(root, relative);
    const info = fs.lstatSync(absolute);
    if (info.isSymbolicLink()) throw new Error(`Linked resource forbidden: ${relative}`);
    if (/(^|\/)(workspace|projects|node_modules|\.git|\.artifacts|trash)(\/|$)|(^|\/)(config\.local\.json|preferences\.md|\.env(?:\..*)?)$/.test(relative)) {
      throw new Error(`Private resource forbidden: ${relative}`);
    }
    if (info.isDirectory()) {
      for (const name of fs.readdirSync(absolute).sort()) walk(`${relative}/${name}`);
    } else if (info.isFile()) {
      names.push(relative);
    } else {
      throw new Error(`Nonregular resource forbidden: ${relative}`);
    }
  }
  for (const relative of ['frontend/dist', 'learning-prompts', 'config/config.example.json', 'LICENSE', 'docs/release/README.md', 'docs/release/THIRD_PARTY_NOTICES.txt']) walk(relative);
  if (!names.includes('frontend/dist/index.html') || !names.includes('learning-prompts/review.md') || !names.includes('learning-prompts/manifest.json')) {
    throw new Error('Release resources are incomplete');
  }
  return names;
}

module.exports = { repoRoot, targets, packagePaths, validateVersion };
