// Gitee ordinary repositories limit each attachment to 100 MB. Publish these
// parts and copy both generated markers into the Gitee Release body.
const fs = require('node:fs');
const path = require('node:path');
function splitMsi(input, output, partBytes = 90 * 1024 * 1024) {
  const name = path.basename(input);
  if (!/^learn-with-ai-\d+\.\d+\.\d+-windows-(amd64|arm64)\.msi$/.test(name)) throw Error('Expected a versioned Learn-With-AI Windows MSI');
  if (!Number.isSafeInteger(partBytes) || partBytes < 1 || partBytes > 90 * 1024 * 1024) throw Error('Invalid part size');
  const stat = fs.lstatSync(input);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size > 512 * 1024 * 1024 || stat.size <= partBytes) throw Error('Expected a regular MSI requiring 2 or more parts, at most 512 MiB');
  const count = Math.ceil(stat.size / partBytes);
  if (count > 16) throw Error('Too many parts');
  fs.mkdirSync(output, { recursive: true });
  const outputs = Array.from({ length: count }, (_, i) => path.join(output, name + '.part' + String(i + 1).padStart(3, '0')));
  const markerFile = path.join(output, name + '.release-marker.txt');
  for (const file of [...outputs, markerFile]) if (fs.existsSync(file)) throw Error('Refusing to overwrite: ' + file);
  const fd = fs.openSync(input, 'r');
  try {
    let position = 0;
    for (const file of outputs) {
      const size = Math.min(partBytes, stat.size - position);
      const buffer = Buffer.alloc(size);
      let read = 0;
      while (read < size) { const n = fs.readSync(fd, buffer, read, size - read, position + read); if (!n) throw Error('MSI changed during split'); read += n; }
      fs.writeFileSync(file, buffer, { flag: 'wx' });
      position += size;
    }
  } finally { fs.closeSync(fd); }
  // Installed versions look for the legacy marker when downloading an upgrade.
  const marker = ['LWA', 'LLL'].map(prefix => `<!-- ${prefix}-UPDATE-PARTS: ${name} ${count} ${stat.size} -->`).join('\n');
  fs.writeFileSync(markerFile, marker + '\n', { flag: 'wx' });
  return { parts: outputs, marker, bytes: stat.size };
}
if (require.main === module) {
  const [input, output] = process.argv.slice(2);
  if (!input || !output) { console.error('Usage: node tools/release/split-gitee-msi.js <MSI> <output-directory>'); process.exit(1); }
  console.log(JSON.stringify(splitMsi(path.resolve(input), path.resolve(output)), null, 2));
}
module.exports = { splitMsi };
