const fs = require('node:fs');
const path = require('node:path');
const { createHash } = require('node:crypto');

function checkPromptResources(root = path.resolve(__dirname, '../..')) {
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'learning-prompts/manifest.json'), 'utf8'));
  for (const resource of manifest.resources) {
    if (!/^learning-prompts\/[a-zA-Z0-9_./-]+$/.test(resource.path) || resource.path.split('/').includes('..')) throw Error('Invalid prompt path');
    const data = fs.readFileSync(path.join(root, resource.path));
    if (data.includes(Buffer.from('\r\n'))) throw Error(`Prompt must use LF before hashing: ${resource.path}`);
    if (createHash('sha256').update(data).digest('hex') !== resource.sha256) throw Error(`Prompt hash mismatch: ${resource.path}`);
  }
  return manifest.resources.length;
}
if (require.main === module) console.log(`prompt resources: PASS (${checkPromptResources()} resources)`);
module.exports = { checkPromptResources };
